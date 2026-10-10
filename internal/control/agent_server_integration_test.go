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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
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

	first, firstSerial := issue()
	second, _ := issue()
	c1, c2 := h.dial(t, &first), h.dial(t, &second)
	if _, err := heartbeat(c1, server.String()); err != nil {
		t.Fatalf("first certificate: %v", err)
	}
	if _, err := heartbeat(c2, server.String()); err != nil {
		t.Fatalf("second certificate: %v", err)
	}

	if ok, err := repo.RevokeCertificate(ctx, firstSerial); err != nil || !ok {
		t.Fatalf("revoke = %v, %v", ok, err)
	}
	if _, err := heartbeat(c1, server.String()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked certificate: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}
	if _, err := heartbeat(c2, server.String()); err != nil {
		t.Fatalf("revoking one certificate must not affect the other: %v", err)
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
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, 'a', 'a')", tenant); err != nil {
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
