//go:build integration

package agentenroll_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type allowAll struct{ err error }

func (a allowAll) RequireServerManageAndConfigSecrets(context.Context, authz.Actor, uuid.UUID) error {
	return a.err
}

type env struct {
	pool   *pgxpool.Pool
	st     *store.Store
	svc    *agentenroll.Service
	tenant uuid.UUID
	server uuid.UUID
	actor  authz.Actor
}

func setup(t *testing.T) *env {
	t.Helper()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	tenant, server, user := seedServer(t, st)
	return &env{
		pool: pool, st: st, tenant: tenant, server: server,
		actor: authz.Actor{UserID: user, Username: "op", TenantID: &tenant},
		svc: &agentenroll.Service{
			Store: st, Authz: allowAll{},
			CA: &agentca.Service{Repo: &agentca.PgRepo{Store: st}, Sealer: sealer},
		},
	}
}

func seedServer(t *testing.T, st *store.Store) (tenant, server, user uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenant, server, user = uuid.New(), uuid.New(), uuid.New()
	site := uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, 'a', 'a')", tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO users (id, tenant_id, username, display_name) VALUES ($1, $2, 'op', 'op')", user, tenant); err != nil {
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
	return tenant, server, user
}

// scan and exec run all-tenants: the pool connection carries no tenant, and RLS would hide
// every row from a bare pool query (and make absence checks pass vacuously).
func (e *env) scan(t *testing.T, sql string, args []any, dest ...any) {
	t.Helper()
	err := e.st.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(dest...)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) exec(t *testing.T, sql string) {
	t.Helper()
	err := e.st.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func csr(t *testing.T) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "x"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func (e *env) createToken(t *testing.T) agentenroll.Token {
	t.Helper()
	tok, err := e.svc.CreateToken(context.Background(), e.actor, e.server)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestCreateTokenReturnsTokenOnceAndStoresOnlyItsHash(t *testing.T) {
	e := setup(t)
	tok := e.createToken(t)
	if len(tok.Value) != 43 || strings.ContainsAny(tok.Value, "+/=") {
		t.Fatalf("token %q is not 32 bytes of unpadded base64url", tok.Value)
	}
	if d := untilExpiry(tok); d < 14*60 || d > 15*60 {
		t.Fatalf("expires in %ds, want ~15 minutes", d)
	}
	sum := sha256.Sum256([]byte(tok.Value))
	var stored string
	var rows int
	e.scan(t, "SELECT count(*), max(token_hash) FROM agent_enroll_tokens", nil, &rows, &stored)
	if rows != 1 || stored != hex.EncodeToString(sum[:]) || strings.Contains(stored, tok.Value) {
		t.Fatalf("rows=%d stored=%q; want one row holding the SHA-256 hex of the token", rows, stored)
	}
	var audits int
	e.scan(t, "SELECT count(*) FROM audit_log WHERE action = 'SERVER_AGENT_ENROLL_TOKEN_CREATED' AND target_id = $1 AND details::text NOT LIKE '%' || $2 || '%'", []any{e.server, tok.Value}, &audits)
	if audits != 1 {
		t.Fatalf("audit rows = %d; want 1 without the token", audits)
	}
}

func untilExpiry(tok agentenroll.Token) int {
	return int(tok.ExpiresAt.Sub(time.Now()).Seconds())
}

func TestCreateTokenRequiresAuthorization(t *testing.T) {
	e := setup(t)
	denied := errors.New("denied")
	e.svc.Authz = allowAll{err: denied}
	if _, err := e.svc.CreateToken(context.Background(), e.actor, e.server); !errors.Is(err, denied) {
		t.Fatalf("err = %v, want the authorization error", err)
	}
	var n int
	e.scan(t, "SELECT count(*) FROM agent_enroll_tokens", nil, &n)
	if n != 0 {
		t.Fatalf("%d tokens stored for a denied request", n)
	}
}

func TestSecondTokenInvalidatesTheFirst(t *testing.T) {
	e := setup(t)
	first := e.createToken(t)
	second := e.createToken(t)
	if _, err := e.svc.Enroll(context.Background(), first.Value, csr(t)); !errors.Is(err, agentenroll.ErrInvalidToken) {
		t.Fatalf("first token after a newer one: err = %v, want ErrInvalidToken", err)
	}
	if _, err := e.svc.Enroll(context.Background(), second.Value, csr(t)); err != nil {
		t.Fatalf("newest token: %v", err)
	}
}

func TestEnrollIssuesCertificateForTheTokensServer(t *testing.T) {
	e := setup(t)
	tok := e.createToken(t)
	res, err := e.svc.Enroll(context.Background(), tok.Value, csr(t))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pem.Decode(res.Issued.CertPEM)
	leaf, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agentca.IdentityFromCert(leaf)
	if err != nil {
		t.Fatal(err)
	}
	if id.TenantID != e.tenant || id.ServerID != e.server {
		t.Fatalf("identity = %+v, want tenant %s server %s", id, e.tenant, e.server)
	}
	if !strings.Contains(string(res.CAPEM), "BEGIN CERTIFICATE") {
		t.Fatal("no CA bundle returned")
	}
	var audits int
	e.scan(t, "SELECT count(*) FROM audit_log WHERE action = 'SERVER_AGENT_ENROLLED' AND target_id = $1", []any{e.server}, &audits)
	if audits != 1 {
		t.Fatalf("enrolled audit rows = %d; want 1", audits)
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	e := setup(t)
	tok := e.createToken(t)
	if _, err := e.svc.Enroll(context.Background(), tok.Value, csr(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Enroll(context.Background(), tok.Value, csr(t)); !errors.Is(err, agentenroll.ErrInvalidToken) {
		t.Fatalf("second use: err = %v, want ErrInvalidToken", err)
	}
}

func TestUnknownUsedAndExpiredTokensFailIdentically(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	used := e.createToken(t)
	if _, err := e.svc.Enroll(ctx, used.Value, csr(t)); err != nil {
		t.Fatal(err)
	}
	expired := e.createToken(t)
	e.exec(t, "UPDATE agent_enroll_tokens SET expires_at = now() - interval '1 second' WHERE used_at IS NULL")
	var msgs []string
	for name, tok := range map[string]string{"unknown": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "used": used.Value, "expired": expired.Value} {
		_, err := e.svc.Enroll(ctx, tok, csr(t))
		if err != agentenroll.ErrInvalidToken {
			t.Fatalf("%s: err = %v, want exactly ErrInvalidToken", name, err)
		}
		msgs = append(msgs, err.Error())
	}
	if msgs[0] != msgs[1] || msgs[1] != msgs[2] {
		t.Fatalf("token failures differ: %q", msgs)
	}
	var certs int
	e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
	if certs != 1 {
		t.Fatalf("%d certificates issued, want only the first enrollment's", certs)
	}
}

func TestMalformedCSRDoesNotConsumeTheToken(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	tok := e.createToken(t)
	if _, err := e.svc.Enroll(ctx, tok.Value, []byte("junk")); !errors.Is(err, agentca.ErrInvalidCSR) {
		t.Fatalf("err = %v, want ErrInvalidCSR", err)
	}
	var used int
	e.scan(t, "SELECT count(*) FROM agent_enroll_tokens WHERE used_at IS NOT NULL", nil, &used)
	if used != 0 {
		t.Fatal("a malformed CSR burned the token")
	}
	if _, err := e.svc.Enroll(ctx, tok.Value, csr(t)); err != nil {
		t.Fatalf("retry with a valid CSR: %v", err)
	}
}

func TestMalformedCSRIsReportedBeforeTheTokenIsChecked(t *testing.T) {
	// The CSR check runs first and reveals nothing about token validity: a bad CSR with an
	// unknown token is a CSR error, not a token oracle.
	e := setup(t)
	if _, err := e.svc.Enroll(context.Background(), "nope", []byte("junk")); !errors.Is(err, agentca.ErrInvalidCSR) {
		t.Fatalf("err = %v, want ErrInvalidCSR", err)
	}
}
