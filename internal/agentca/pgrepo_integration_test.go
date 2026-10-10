//go:build integration

package agentca_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func csrPEM(t *testing.T) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "x"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestPgRepoCAAndCertificates(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, _ := secrets.NewSealer(key)
	repo := &agentca.PgRepo{Store: st}
	svc := &agentca.Service{Repo: repo, Sealer: sealer}

	// Concurrent first callers converge on one CA.
	var wg sync.WaitGroup
	cas := make([]*agentca.CA, 8)
	for i := range cas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ca, err := svc.LoadOrCreateCA(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			cas[i] = ca
		}()
	}
	wg.Wait()
	for i, ca := range cas {
		if ca == nil || !ca.Cert.Equal(cas[0].Cert) {
			t.Fatalf("caller %d got a different CA", i)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent_ca").Scan(&n); err != nil || n != 1 {
		t.Fatalf("agent_ca rows = %d, %v; want 1", n, err)
	}
	rec, err := repo.GetCA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := cas[0].MarshalKey()
	if string(rec.KeySealed) == string(der) || len(rec.KeySealed) <= len(der) {
		t.Fatal("CA key not sealed")
	}

	// Issued certificates need real tenant and server rows.
	tenantA, serverA := seedServer(t, ctx, st, "a")
	tenantB, _ := seedServer(t, ctx, st, "b")
	issuance, err := svc.Issue(ctx, csrPEM(t), agentca.Identity{TenantID: tenantA, ServerID: serverA})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCertificate(ctx, issuance.Issued.Serial)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != tenantA || got.ServerID != serverA || got.Fingerprint != issuance.Issued.Fingerprint || got.RevokedAt != nil {
		t.Fatalf("stored certificate = %+v", got)
	}

	// Row-level security: another tenant cannot see tenant A's certificate.
	var seen int
	err = st.TxRaw(ctx, store.TenantScope{TenantID: tenantB}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM agent_certificates").Scan(&seen)
	})
	if err != nil || seen != 0 {
		t.Fatalf("tenant B sees %d certificates, %v", seen, err)
	}
	// And cannot insert one for tenant A.
	err = st.Tx(ctx, store.TenantScope{TenantID: tenantB}, func(q *db.Queries) error {
		return q.InsertAgentCertificate(ctx, db.InsertAgentCertificateParams{
			Serial: "ff", TenantID: tenantA, ServerID: serverA, Fingerprint: "f", NotBefore: got.NotBefore, NotAfter: got.NotAfter,
		})
	})
	if err == nil {
		t.Fatal("tenant B inserted a certificate for tenant A")
	}

	// A server can only be issued under the tenant that owns it.
	if _, err := svc.Issue(ctx, csrPEM(t), agentca.Identity{TenantID: tenantB, ServerID: serverA}); err == nil {
		t.Fatal("issued a certificate for tenant A's server under tenant B")
	}
	// Count across all tenants: a bare pool query would see nothing under FORCE RLS.
	var crossed, own int
	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM agent_certificates WHERE tenant_id = $1", tenantA).Scan(&own); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM agent_certificates WHERE tenant_id = $1", tenantB).Scan(&crossed)
	})
	if err != nil || own != 1 {
		t.Fatalf("tenant A certificates = %d, %v; want 1 (the count must see rows)", own, err)
	}
	if crossed != 0 {
		t.Fatalf("cross-tenant certificates = %d, %v; want 0", crossed, err)
	}

	if _, err := repo.GetCertificate(ctx, "does-not-exist"); !errors.Is(err, agentca.ErrCertificateNotFound) {
		t.Fatalf("err = %v, want ErrCertificateNotFound", err)
	}
}

func seedServer(t *testing.T, ctx context.Context, st *store.Store, name string) (tenant, server uuid.UUID) {
	t.Helper()
	tenant, server = uuid.New(), uuid.New()
	site := uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $2)", tenant, name); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, $3)", site, tenant, name); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, $4, 'http://x', 'u', '\\x00')",
			server, tenant, site, name)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tenant, server
}

func TestRevokeCertificateAndServer(t *testing.T) {
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
	tenant, server := seedServer(t, ctx, st, "r")
	id := agentca.Identity{TenantID: tenant, ServerID: server}
	issue := func() string {
		res, err := svc.Issue(ctx, csrPEM(t), id)
		if err != nil {
			t.Fatal(err)
		}
		return res.Issued.Serial
	}

	first, second, third := issue(), issue(), issue()
	if ok, err := repo.RevokeCertificate(ctx, first); err != nil || !ok {
		t.Fatalf("RevokeCertificate = %v, %v; want true", ok, err)
	}
	got, err := repo.GetCertificate(ctx, first)
	if err != nil || got.RevokedAt == nil {
		t.Fatalf("after revoke: %+v, %v", got, err)
	}
	revokedAt := *got.RevokedAt
	if ok, err := repo.RevokeCertificate(ctx, first); err != nil || ok {
		t.Fatalf("second revoke = %v, %v; want false (already revoked)", ok, err)
	}
	if ok, err := repo.RevokeCertificate(ctx, "unknown"); err != nil || ok {
		t.Fatalf("revoking an unknown serial = %v, %v; want false", ok, err)
	}
	// Revoking the server revokes the remaining live certificates and keeps the first date.
	n, err := repo.RevokeServerCertificates(ctx, server)
	if err != nil || n != 2 {
		t.Fatalf("RevokeServerCertificates = %d, %v; want 2", n, err)
	}
	for _, serial := range []string{second, third} {
		if c, err := repo.GetCertificate(ctx, serial); err != nil || c.RevokedAt == nil {
			t.Fatalf("serial %s not revoked: %+v, %v", serial, c, err)
		}
	}
	if c, _ := repo.GetCertificate(ctx, first); !c.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revocation date of an already revoked certificate moved: %s -> %s", revokedAt, c.RevokedAt)
	}
}
