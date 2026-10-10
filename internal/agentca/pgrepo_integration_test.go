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

func newRepo(t *testing.T) (*store.Store, *agentca.PgRepo, *agentca.Service) {
	t.Helper()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	repo := &agentca.PgRepo{Store: st}
	return st, repo, &agentca.Service{Repo: repo, Sealer: sealer}
}

// ActivateCertificate is the first-use hook: it marks the certificate used and revokes every
// other live certificate of the server, once, and leaves other servers alone.
func TestActivateCertificateRevokesTheServersOtherCertificates(t *testing.T) {
	ctx := context.Background()
	st, repo, svc := newRepo(t)
	tenant, server := seedServer(t, ctx, st, "act")
	otherTenant, otherServer := seedServer(t, ctx, st, "act2")
	id := agentca.Identity{TenantID: tenant, ServerID: server}
	issue := func(id agentca.Identity) agentca.Certificate {
		res, err := svc.Issue(ctx, csrPEM(t), id)
		if err != nil {
			t.Fatal(err)
		}
		c, err := repo.GetCertificate(ctx, res.Issued.Serial)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := issue(id), issue(id)
	other := issue(agentca.Identity{TenantID: otherTenant, ServerID: otherServer})
	if a.FirstUsedAt != nil || a.ParentSerial != "" {
		t.Fatalf("a new certificate = %+v, want unused and parentless", a)
	}

	if err := repo.ActivateCertificate(ctx, a); err != nil {
		t.Fatal(err)
	}
	got := func(c agentca.Certificate) agentca.Certificate {
		r, err := repo.GetCertificate(ctx, c.Serial)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if g := got(a); g.FirstUsedAt == nil || g.RevokedAt != nil {
		t.Fatalf("activated certificate = %+v, want used and live", g)
	}
	if g := got(b); g.RevokedAt == nil {
		t.Fatal("activation left the server's other certificate live")
	}
	if g := got(other); g.RevokedAt != nil {
		t.Fatal("activation revoked another server's certificate")
	}
	// Idempotent: a second activation changes nothing.
	first := *got(a).FirstUsedAt
	if err := repo.ActivateCertificate(ctx, a); err != nil {
		t.Fatal(err)
	}
	if !got(a).FirstUsedAt.Equal(first) {
		t.Fatal("a second activation moved first_used_at")
	}
	// A serial with no row is refused like a revoked certificate.
	ghost := a
	ghost.Serial = "ffffffff"
	if err := repo.ActivateCertificate(ctx, ghost); !errors.Is(err, agentca.ErrCertificateRevoked) {
		t.Fatalf("activating an unknown serial: %v, want ErrCertificateRevoked", err)
	}
	// A revoked certificate cannot be activated.
	if err := repo.ActivateCertificate(ctx, b); !errors.Is(err, agentca.ErrCertificateRevoked) {
		t.Fatalf("activating a revoked certificate: %v, want ErrCertificateRevoked", err)
	}
	// Explicit revocation still works on an activated certificate.
	if ok, err := repo.RevokeCertificate(ctx, a.Serial); err != nil || !ok {
		t.Fatalf("RevokeCertificate = %v, %v", ok, err)
	}
}

// The migration adds the renewal columns and leaves row-level security in force.
func TestRenewalColumnsAndRLS(t *testing.T) {
	ctx := context.Background()
	st, _, _ := newRepo(t)
	var cols int
	var rls, forced bool
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'agent_certificates' AND column_name IN ('parent_serial', 'first_used_at')").Scan(&cols); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = 'agent_certificates'").Scan(&rls, &forced)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cols != 2 || !rls || !forced {
		t.Fatalf("columns=%d rls=%v forced=%v; want both columns with RLS enabled and forced", cols, rls, forced)
	}
}
