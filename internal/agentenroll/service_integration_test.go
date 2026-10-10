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
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
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
	url    string // superuser URL, for fault injection only
	st     *store.Store
	svc    *agentenroll.Service
	ca     *agentca.Service
	tenant uuid.UUID
	server uuid.UUID
	actor  authz.Actor
}

func setup(t *testing.T) *env {
	t.Helper()
	pool, url := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	tenant, server, user := seedServer(t, st)
	ca := &agentca.Service{Repo: &agentca.PgRepo{Store: st}, Sealer: sealer}
	return &env{
		pool: pool, url: url, st: st, tenant: tenant, server: server, ca: ca,
		actor: authz.Actor{UserID: user, Username: "op", TenantID: &tenant},
		svc:   &agentenroll.Service{Store: st, Authz: allowAll{}, CA: ca},
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
	// Both events target the server (a uuid column) under the same target type; the
	// issued certificate is identified in the details.
	var targetType, serial string
	e.scan(t, "SELECT target_type, details->>'serial' FROM audit_log WHERE action = 'SERVER_AGENT_ENROLLED'", nil, &targetType, &serial)
	if targetType != "server_agent" || serial != res.Issued.Serial {
		t.Fatalf("enrolled audit target_type=%q serial=%q; want server_agent and %q", targetType, serial, res.Issued.Serial)
	}
	e.scan(t, "SELECT target_type FROM audit_log WHERE action = 'SERVER_AGENT_ENROLL_TOKEN_CREATED' AND target_id = $1", []any{e.server}, &targetType)
	if targetType != "server_agent" {
		t.Fatalf("token audit target_type=%q; want server_agent", targetType)
	}
}

// admin runs sql as the superuser: the application role may not create functions or
// triggers, and fault injection must not go through the code under test.
func (e *env) admin(t *testing.T, sql string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
}

// failOn makes every INSERT into table raise, standing in for a database fault at that step
// of the enrollment transaction.
func (e *env) failOn(t *testing.T, table string) {
	t.Helper()
	e.admin(t, "CREATE OR REPLACE FUNCTION fail_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$")
	e.admin(t, "CREATE TRIGGER fail_insert BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION fail_insert()")
}

func (e *env) clearFailure(t *testing.T, table string) {
	t.Helper()
	e.admin(t, "DROP TRIGGER fail_insert ON "+table)
}

// An enrollment is all or nothing: when the certificate insert or the audit insert fails,
// no certificate exists, the token is not burned and the agent can retry with the same token.
func TestEnrollIsAtomicWhenAStepFails(t *testing.T) {
	for _, tc := range []struct{ name, table string }{
		{"audit insert fails", "audit_log"},
		{"certificate insert fails", "agent_certificates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t)
			ctx := context.Background()
			tok := e.createToken(t)
			e.failOn(t, tc.table)
			if _, err := e.svc.Enroll(ctx, tok.Value, csr(t)); err == nil {
				t.Fatal("enroll succeeded despite the injected failure")
			} else if errors.Is(err, agentenroll.ErrInvalidToken) {
				t.Fatalf("a database fault must not look like a bad token: %v", err)
			}
			var certs, used int
			e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
			e.scan(t, "SELECT count(*) FROM agent_enroll_tokens WHERE used_at IS NOT NULL", nil, &used)
			if certs != 0 || used != 0 {
				t.Fatalf("after a failed enrollment: %d certificates, %d consumed tokens; want 0 and 0", certs, used)
			}
			e.clearFailure(t, tc.table)
			if _, err := e.svc.Enroll(ctx, tok.Value, csr(t)); err != nil {
				t.Fatalf("retry with the same token: %v", err)
			}
			e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
			if certs != 1 {
				t.Fatalf("%d certificates after the retry, want 1", certs)
			}
		})
	}
}

// Concurrent token creation for one server leaves exactly one live token, and it redeems.
func TestConcurrentCreateTokenLeavesOneLiveToken(t *testing.T) {
	e := setup(t)
	const n = 8
	toks := make([]agentenroll.Token, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range toks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			toks[i], errs[i] = e.svc.CreateToken(context.Background(), e.actor, e.server)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateToken #%d: %v", i, err)
		}
	}
	var live int
	e.scan(t, "SELECT count(*) FROM agent_enroll_tokens WHERE server_id = $1 AND used_at IS NULL", []any{e.server}, &live)
	if live != 1 {
		t.Fatalf("%d live tokens after %d concurrent creations, want exactly 1", live, n)
	}
	redeemed := 0
	for _, tok := range toks {
		if _, err := e.svc.Enroll(context.Background(), tok.Value, csr(t)); err == nil {
			redeemed++
		} else if !errors.Is(err, agentenroll.ErrInvalidToken) {
			t.Fatal(err)
		}
	}
	if redeemed != 1 {
		t.Fatalf("%d of the returned tokens redeemed, want exactly 1", redeemed)
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

// notFoundIssuer loads the CA through the real service but fails every Sign with an error
// that wraps store.ErrNotFound, the way a lookup deep inside signing or recording could.
type notFoundIssuer struct{ *agentca.Service }

func (notFoundIssuer) Sign(*agentca.CA, []byte, agentca.Identity) (*agentca.Issuance, error) {
	return nil, fmt.Errorf("a later step: %w", store.ErrNotFound)
}

// Only the token lookup means "invalid token". A not-found raised by a later step is an
// internal error: it must not be reported as a bad token, and it must not burn the token.
func TestNotFoundAfterConsumeIsNotAnInvalidToken(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	tok := e.createToken(t)
	genuine := e.svc.CA
	e.svc.CA = notFoundIssuer{genuine.(*agentca.Service)}
	_, err := e.svc.Enroll(ctx, tok.Value, csr(t))
	if err == nil || errors.Is(err, agentenroll.ErrInvalidToken) {
		t.Fatalf("err = %v, want an internal error that is not ErrInvalidToken", err)
	}
	e.svc.CA = genuine
	if _, err := e.svc.Enroll(ctx, tok.Value, csr(t)); err != nil {
		t.Fatalf("token after the rolled-back attempt: %v", err)
	}
}

// With a cold CA cache and a tiny pool, concurrent first enrollments must not need a second
// pool connection while the enrollment transaction holds one.
func TestConcurrentFirstEnrollmentsDoNotExhaustThePool(t *testing.T) {
	e := setup(t)
	const n = 4
	toks := make([]agentenroll.Token, n)
	for i := range toks {
		// One live token per server, so give each enrollment its own server.
		if i == 0 {
			toks[i] = e.createToken(t)
			continue
		}
		toks[i] = e.createTokenForNewServer(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	small, err := postgres.Connect(ctx, e.url+"&pool_max_conns=2")
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	st := &store.Store{Pool: small}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	svc := &agentenroll.Service{Store: st, Authz: allowAll{},
		CA: &agentca.Service{Repo: &agentca.PgRepo{Store: st}, Sealer: sealer}}
	// Built here: t.Fatal must not run on a goroutine other than the test's.
	csrs := make([][]byte, n)
	for i := range csrs {
		csrs[i] = csr(t)
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range toks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.Enroll(ctx, toks[i].Value, csrs[i])
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("enrollment #%d: %v", i, err)
		}
	}
}

func (e *env) createTokenForNewServer(t *testing.T) agentenroll.Token {
	t.Helper()
	ctx := context.Background()
	server, site := uuid.New(), uuid.New()
	err := e.st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, $3)", site, e.tenant, site.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, $4, 'http://x', 'u', '\\x00')",
			server, e.tenant, site, server.String())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := e.svc.CreateToken(ctx, e.actor, server)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// The expiry comes from the database clock, and replacing the live token refreshes its
// expiry, creator and creation time.
func TestCreateTokenExpiryAndReplacementRefreshTheRow(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.TTL = time.Hour
	first := e.createToken(t)
	if d := time.Until(first.ExpiresAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Fatalf("expires in %s, want ~1h", d)
	}
	var firstCreatedAt time.Time
	e.scan(t, "SELECT created_at FROM agent_enroll_tokens WHERE server_id = $1", []any{e.server}, &firstCreatedAt)

	other := uuid.New()
	e.exec(t, "INSERT INTO users (id, tenant_id, username, display_name) VALUES ('"+other.String()+"', '"+e.tenant.String()+"', 'op2', 'op2')")
	e.svc.TTL = 2 * time.Hour
	second, err := e.svc.CreateToken(ctx, authz.Actor{UserID: other, Username: "op2", TenantID: &e.tenant}, e.server)
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	var expires, createdAt time.Time
	var createdBy uuid.UUID
	e.scan(t, "SELECT count(*), max(expires_at), max(created_at), max(created_by::text)::uuid FROM agent_enroll_tokens WHERE server_id = $1 AND used_at IS NULL",
		[]any{e.server}, &rows, &expires, &createdAt, &createdBy)
	if rows != 1 || createdBy != other || !createdAt.After(firstCreatedAt) || expires.Sub(first.ExpiresAt) < 59*time.Minute || !expires.Equal(second.ExpiresAt) {
		t.Fatalf("rows=%d created_by=%s (want %s) created_at refreshed=%v expires=%s (first %s, returned %s)",
			rows, createdBy, other, createdAt.After(firstCreatedAt), expires, first.ExpiresAt, second.ExpiresAt)
	}
}

// enrollAged enrolls the server's agent with a certificate issued age ago, so it is that far
// into its 30 day lifetime. The issuer clock is restored afterwards.
func (e *env) enrollAged(t *testing.T, age time.Duration) *agentca.Issuance {
	t.Helper()
	e.ca.Now = func() time.Time { return time.Now().Add(-age) }
	defer func() { e.ca.Now = nil }()
	res, err := e.svc.Enroll(context.Background(), e.createToken(t).Value, csr(t))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *env) identity() agentca.Identity {
	return agentca.Identity{TenantID: e.tenant, ServerID: e.server}
}

// liveCertificates counts the server's unexpired certificates that are not revoked at all.
func (e *env) unrevoked(t *testing.T) int {
	t.Helper()
	var n int
	e.scan(t, "SELECT count(*) FROM agent_certificates WHERE server_id = $1 AND revoked_at IS NULL", []any{e.server}, &n)
	return n
}

func TestRenewIssuesAFreshCertificateAndSupersedesThePreviousOne(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	first := e.enrollAged(t, 20*24*time.Hour)

	res, err := e.svc.Renew(ctx, e.identity(), first.Issued.Serial, csr(t))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pem.Decode(res.Issued.CertPEM)
	leaf, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := agentca.IdentityFromCert(leaf); err != nil || got != e.identity() {
		t.Fatalf("renewed identity = %+v, %v; want %+v", got, err, e.identity())
	}
	if res.Issued.Serial == first.Issued.Serial {
		t.Fatal("renewal reused the serial")
	}

	// The new certificate is unrevoked; the old one is revoked from now + grace, not at once.
	var certs int
	e.scan(t, "SELECT count(*) FROM agent_certificates WHERE server_id = $1", []any{e.server}, &certs)
	if certs != 2 || e.unrevoked(t) != 1 {
		t.Fatalf("%d certificates, %d unrevoked; want 2 and 1", certs, e.unrevoked(t))
	}
	var untilRevoked float64
	e.scan(t, "SELECT extract(epoch FROM revoked_at - now()) FROM agent_certificates WHERE serial = $1", []any{first.Issued.Serial}, &untilRevoked)
	grace := agentenroll.SupersedeGrace.Seconds()
	if untilRevoked < grace-60 || untilRevoked > grace {
		t.Fatalf("old certificate is revoked in %.0fs, want about the %.0fs grace", untilRevoked, grace)
	}
	var targetType, renewed, previous string
	var superseded int
	e.scan(t, "SELECT target_type, details->>'serial', details->>'previous_serial', (details->>'superseded')::int FROM audit_log WHERE action = 'SERVER_AGENT_CERT_RENEWED' AND target_id = $1",
		[]any{e.server}, &targetType, &renewed, &previous, &superseded)
	if targetType != "server_agent" || renewed != res.Issued.Serial || previous != first.Issued.Serial || superseded != 1 {
		t.Fatalf("audit = %q serial %q previous %q superseded %d; want server_agent %q %q 1", targetType, renewed, previous, superseded, res.Issued.Serial, first.Issued.Serial)
	}
}

func TestRenewRefusesBeforeHalfTheLifetime(t *testing.T) {
	for name, age := range map[string]time.Duration{"just issued": 0, "14 of 30 days": 14 * 24 * time.Hour} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			first := e.enrollAged(t, age)
			_, err := e.svc.Renew(context.Background(), e.identity(), first.Issued.Serial, csr(t))
			if !errors.Is(err, agentca.ErrRenewalTooEarly) {
				t.Fatalf("err = %v, want ErrRenewalTooEarly", err)
			}
			var certs, audits int
			e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
			e.scan(t, "SELECT count(*) FROM audit_log WHERE action = 'SERVER_AGENT_CERT_RENEWED'", nil, &audits)
			if certs != 1 || audits != 0 || e.unrevoked(t) != 1 {
				t.Fatalf("%d certificates, %d audits, %d unrevoked after a refused renewal; want 1, 0, 1", certs, audits, e.unrevoked(t))
			}
		})
	}
	// Positive control for the boundary: 16 of 30 days is past the half.
	e := setup(t)
	first := e.enrollAged(t, 16*24*time.Hour)
	if _, err := e.svc.Renew(context.Background(), e.identity(), first.Issued.Serial, csr(t)); err != nil {
		t.Fatalf("16 of 30 days: %v", err)
	}
}

func TestRenewRetryWithinGraceSucceedsAndBoundsTheLiveCertificates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	first := e.enrollAged(t, 20*24*time.Hour)
	if _, err := e.svc.Renew(ctx, e.identity(), first.Issued.Serial, csr(t)); err != nil {
		t.Fatal(err)
	}
	// The response was lost: the agent still holds the first certificate, which is in its grace
	// period, and asks again.
	second, err := e.svc.Renew(ctx, e.identity(), first.Issued.Serial, csr(t))
	if err != nil {
		t.Fatalf("retry within the grace period: %v", err)
	}
	// Exactly one certificate is unrevoked (the newest); everything else ends within the grace.
	var certs, beyondGrace int
	e.scan(t, "SELECT count(*), count(*) FILTER (WHERE revoked_at > now() + make_interval(secs => $2)) FROM agent_certificates WHERE server_id = $1",
		[]any{e.server, agentenroll.SupersedeGrace.Seconds() + 1}, &certs, &beyondGrace)
	var unrevokedSerial string
	e.scan(t, "SELECT serial FROM agent_certificates WHERE server_id = $1 AND revoked_at IS NULL", []any{e.server}, &unrevokedSerial)
	if certs != 3 || beyondGrace != 0 || unrevokedSerial != second.Issued.Serial {
		t.Fatalf("%d certificates, %d beyond the grace, unrevoked %s; want 3, 0 and the newest %s", certs, beyondGrace, unrevokedSerial, second.Issued.Serial)
	}
	// Once the grace is over, the old certificate can no longer renew.
	e.exec(t, "UPDATE agent_certificates SET revoked_at = now() - interval '1 second' WHERE serial = '"+first.Issued.Serial+"'")
	if _, err := e.svc.Renew(ctx, e.identity(), first.Issued.Serial, csr(t)); !errors.Is(err, agentca.ErrCertificateNotFound) {
		t.Fatalf("renewing with a revoked certificate: %v, want ErrCertificateNotFound", err)
	}
}

func TestRenewRejectsABadCSRWithoutSideEffects(t *testing.T) {
	e := setup(t)
	if _, err := e.svc.Renew(context.Background(), e.identity(), "ab", []byte("not a csr")); !errors.Is(err, agentca.ErrInvalidCSR) {
		t.Fatalf("err = %v, want ErrInvalidCSR", err)
	}
	var certs, audits int
	e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
	e.scan(t, "SELECT count(*) FROM audit_log WHERE action = 'SERVER_AGENT_CERT_RENEWED'", nil, &audits)
	if certs != 0 || audits != 0 {
		t.Fatalf("%d certificates, %d audits after a refused renewal; want 0", certs, audits)
	}
}

// Renewal is all or nothing, like enrollment: a failed audit write leaves no new certificate
// and does not supersede the old one.
func TestRenewIsAtomicWhenTheAuditFails(t *testing.T) {
	e := setup(t)
	first := e.enrollAged(t, 20*24*time.Hour)
	e.failOn(t, "audit_log")
	_, err := e.svc.Renew(context.Background(), e.identity(), first.Issued.Serial, csr(t))
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v, want the injected audit failure", err)
	}
	var certs int
	e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
	if certs != 1 || e.unrevoked(t) != 1 {
		t.Fatalf("%d certificates, %d unrevoked after a failed renewal; want 1 and 1 (nothing superseded)", certs, e.unrevoked(t))
	}
	// Positive control: without the fault the same renewal works.
	e.clearFailure(t, "audit_log")
	if _, err := e.svc.Renew(context.Background(), e.identity(), first.Issued.Serial, csr(t)); err != nil {
		t.Fatalf("renewal without the fault: %v", err)
	}
}

// A certificate cannot be renewed into another tenant's server: the presenting certificate must
// belong to the identity's own server, and nothing is written otherwise.
func TestRenewCannotCrossTenants(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	first := e.enrollAged(t, 20*24*time.Hour)
	otherTenant, otherServer := seedOtherTenant(t, e)

	crossed := agentca.Identity{TenantID: e.tenant, ServerID: otherServer}
	if _, err := e.svc.Renew(ctx, crossed, first.Issued.Serial, csr(t)); !errors.Is(err, agentca.ErrCertificateNotFound) {
		t.Fatalf("crossed server: %v, want ErrCertificateNotFound", err)
	}
	foreign := agentca.Identity{TenantID: otherTenant, ServerID: e.server}
	if _, err := e.svc.Renew(ctx, foreign, first.Issued.Serial, csr(t)); !errors.Is(err, agentca.ErrCertificateNotFound) {
		t.Fatalf("foreign tenant: %v, want ErrCertificateNotFound", err)
	}
	var certs int
	e.scan(t, "SELECT count(*) FROM agent_certificates", nil, &certs)
	if certs != 1 || e.unrevoked(t) != 1 {
		t.Fatalf("%d certificates, %d unrevoked after refused renewals; want 1 and 1", certs, e.unrevoked(t))
	}
	// Positive control: the same certificate renews under its own identity.
	if _, err := e.svc.Renew(ctx, e.identity(), first.Issued.Serial, csr(t)); err != nil {
		t.Fatalf("own identity: %v", err)
	}
}

func seedOtherTenant(t *testing.T, e *env) (tenant, server uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenant, server, site := uuid.New(), uuid.New(), uuid.New()
	err := e.st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, 'b', 'b')", tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'b')", site, tenant); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, 'b', 'http://y', 'u', '\\x00')",
			server, tenant, site)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tenant, server
}
