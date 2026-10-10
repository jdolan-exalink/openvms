//go:build integration

package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// The whole path against Postgres: a certificate issued by the real agentca.Service is
// accepted, and revoking it through PgRepo takes effect on the next call.
func TestAgentListener_RealCertificatesAndRevocation(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	repo := &agentca.PgRepo{Store: st}
	svc := &agentca.Service{Repo: repo, Sealer: sealer}
	ca, err := svc.LoadOrCreateCA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tenant, server := seedAgentServer(t, ctx, st)
	id := agentca.Identity{TenantID: tenant, ServerID: server}

	issue := func() (tls.Certificate, string) {
		priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "agent"}}, priv)
		if err != nil {
			t.Fatal(err)
		}
		res, err := svc.Issue(ctx, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), id)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, _ := x509.MarshalECPrivateKey(priv)
		pair, err := tls.X509KeyPair(res.Issued.CertPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
		if err != nil {
			t.Fatal(err)
		}
		return pair, res.Issued.Serial
	}

	pool2 := x509.NewCertPool()
	pool2.AddCert(ca.Cert)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pool2)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewAgentServer(AgentConfig{Credentials: creds, Verifier: &agentauth.Verifier{Certs: repo}})
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.GRPCServer().Serve(lis) }()
	t.Cleanup(srv.Stop)
	h := &agentHarness{addr: lis.Addr().String(), serverCA: certFile}

	// A server has one live certificate once the agent uses it (a certificate's first call revokes
	// the server's others; renewal is covered below). Revoking works on a used certificate.
	first, firstSerial := issue()
	c1 := h.nodes(t, &first)
	if _, err := heartbeat(c1, server.String()); err != nil {
		t.Fatalf("first certificate: %v", err)
	}
	if ok, err := repo.RevokeCertificate(ctx, firstSerial); err != nil || !ok {
		t.Fatalf("revoke = %v, %v", ok, err)
	}
	if _, err := heartbeat(c1, server.String()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked certificate: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}

	second, _ := issue()
	c2 := h.nodes(t, &second)
	if _, err := heartbeat(c2, server.String()); err != nil {
		t.Fatalf("a new certificate after the first was revoked: %v", err)
	}
	if n, err := repo.RevokeServerCertificates(ctx, server); err != nil || n != 1 {
		t.Fatalf("revoke server = %d, %v; want 1 live certificate", n, err)
	}
	if _, err := heartbeat(c2, server.String()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("after revoking the server: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}
}

func seedAgentServer(t *testing.T, ctx context.Context, st *store.Store) (tenant, server uuid.UUID) {
	t.Helper()
	tenant, server = uuid.New(), uuid.New()
	site := uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $2)", tenant, tenant.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'a')", site, tenant); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, 'a', 'http://x', 'u', '\\x00')",
			server, tenant, site)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tenant, server
}

// Renewal against Postgres, end to end over mutual TLS. A renewal supersedes the old certificate
// only when the agent first uses the new one:
//   - a lost response leaves the old certificate working, and renewing again replaces the unused
//     successor;
//   - the successor's first call revokes its parent and every other certificate of the server;
//   - after that the parent (a stolen copy of the old key) can neither call nor renew, and the
//     legitimate agent is unaffected.
func TestAgentListener_RenewalSupersedesOnFirstUse(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	repo := &agentca.PgRepo{Store: st}
	svc := &agentca.Service{Repo: repo, Sealer: sealer}
	ca, err := svc.LoadOrCreateCA(ctx) // created with the real clock
	if err != nil {
		t.Fatal(err)
	}
	tenant, server := seedAgentServer(t, ctx, st)
	id := agentca.Identity{TenantID: tenant, ServerID: server}

	// A certificate already 20 of 30 days old, so it may renew; a fresh one may not.
	issueAt := func(id agentca.Identity, age time.Duration) (tls.Certificate, string) {
		svc.Now = func() time.Time { return time.Now().Add(-age) }
		defer func() { svc.Now = nil }()
		keyPair, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		csrDER, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "agent"}}, keyPair)
		res, err := svc.Issue(ctx, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), id)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, _ := x509.MarshalECPrivateKey(keyPair)
		pair, err := tls.X509KeyPair(res.Issued.CertPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
		if err != nil {
			t.Fatal(err)
		}
		return pair, res.Issued.Serial
	}
	old, _ := issueAt(id, 20*24*time.Hour)

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(ca.Cert)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, clientCAs)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewAgentServer(AgentConfig{
		Credentials: creds, Verifier: &agentauth.Verifier{Certs: repo},
		Renewer: &agentenroll.Service{Store: st, CA: svc},
	})
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.GRPCServer().Serve(lis) }()
	t.Cleanup(srv.Stop)
	h := &agentHarness{addr: lis.Addr().String(), serverCA: certFile}

	renewWith := func(from tls.Certificate) (tls.Certificate, error) {
		csr, newKey := newCSR(t)
		resp, err := renew(h, t, &from, csr)
		if err != nil {
			return tls.Certificate{}, err
		}
		block, _ := pem.Decode([]byte(resp.CertificatePem))
		return tls.Certificate{Certificate: [][]byte{block.Bytes}, PrivateKey: newKey}, nil
	}
	alive := func(c tls.Certificate) error { _, err := heartbeat(h.nodes(t, &c), server.String()); return err }

	// The old certificate's first call activates it (it is the agent's current identity).
	if err := alive(old); err != nil {
		t.Fatalf("old certificate: %v", err)
	}
	// Too young: a fresh certificate (of another server, so it does not supersede the old one)
	// cannot renew yet.
	tenant2, server2 := seedAgentServer(t, ctx, st)
	young, _ := issueAt(agentca.Identity{TenantID: tenant2, ServerID: server2}, 0)
	if _, err := renewWith(young); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("young certificate: code = %s (%v), want FailedPrecondition", status.Code(err), err)
	}

	// Lost response: the agent never learns of c1 and renews again from the old certificate.
	c1, err := renewWith(old)
	if err != nil {
		t.Fatalf("renewal: %v", err)
	}
	if err := alive(old); err != nil {
		t.Fatalf("old certificate while its successor is unused: %v", err)
	}
	c2, err := renewWith(old)
	if err != nil {
		t.Fatalf("renewal retry from the old certificate: %v", err)
	}
	if err := alive(c1); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("the replaced unused successor: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}

	// The agent switches to c2. Its first call revokes the old certificate.
	if err := alive(c2); err != nil {
		t.Fatalf("new certificate: %v", err)
	}
	if err := alive(old); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("old certificate after the successor was used: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}
	// A stolen copy of the old key can no longer renew, and nothing it does touches c2.
	if _, err := renewWith(old); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("renewal from the superseded certificate: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}
	if err := alive(c2); err != nil {
		t.Fatalf("the legitimate agent must be unaffected: %v", err)
	}
}
