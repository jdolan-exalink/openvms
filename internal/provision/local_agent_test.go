package provision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestRegisterLocalAgentAuthorizesBeforeTransaction(t *testing.T) {
	denied := errors.New("forbidden")
	transactions := 0
	svc := &Service{
		requireLocalAgentPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return denied },
		localAgentTx: func(context.Context, authz.Actor, uuid.UUID, []byte, time.Duration, func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error {
			transactions++
			return nil
		},
	}
	_, _, ca := localAgentTestMaterial(t)
	err := svc.RegisterLocalAgent(context.Background(), localAgentTestActor(uuid.New()), localAgentTestSessionHash, time.Hour, localAgentTestInput(uuid.New(), ca), localAgentTestToken)
	if !errors.Is(err, denied) {
		t.Fatalf("RegisterLocalAgent() error = %v, want authorization denial", err)
	}
	if transactions != 0 {
		t.Fatalf("transaction count = %d, want no write transaction after denial", transactions)
	}
}

func TestRegisterLocalAgentSealsTokenAndWritesAgentTLSAuditAtomically(t *testing.T) {
	sealer, serverID, ca := localAgentTestMaterial(t)
	tenantID := uuid.New()
	actor := localAgentTestActor(tenantID)
	queries := &fakeLocalAgentRegistrationQueries{insertRows: 1}
	committed := false
	svc := localAgentService(sealer, tenantID, queries, &committed)
	if err := svc.RegisterLocalAgent(context.Background(), actor, localAgentTestSessionHash, time.Hour, localAgentTestInput(serverID, ca), "  "+localAgentTestToken+"\n"); err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("registration transaction did not commit")
	}
	if queries.agent == nil || queries.agent.ServerID != serverID || queries.agent.TenantID != tenantID || queries.agent.Host != "192.0.2.25" || queries.agent.Port != 7419 {
		t.Fatalf("agent registration fields = %#v", queries.agent)
	}
	opened, err := sealer.Open(queries.agent.TokenSealed, serverID[:])
	if err != nil || string(opened) != localAgentTestToken {
		t.Fatalf("sealed token failed round trip: token=%q err=%v", opened, err)
	}
	if queries.tls == nil || queries.tls.TenantID != tenantID || queries.tls.SecurePort != 7443 || queries.tls.TrustMode != AgentTLSTrustCustom || queries.tls.CaPem == nil || *queries.tls.CaPem != string(ca) {
		t.Fatalf("TLS registration fields = %#v", queries.tls)
	}
	if queries.audit == nil || queries.audit.TenantID == nil || *queries.audit.TenantID != tenantID || queries.audit.ActorName != actor.Username {
		t.Fatalf("audit identity = %#v", queries.audit)
	}
	var details map[string]any
	if err := json.Unmarshal(queries.audit.Details, &details); err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256.Sum256(ca)
	if details["ca_bundle_sha256"] != hex.EncodeToString(fingerprint[:]) || details["host"] != "192.0.2.25" || details["http_port"] != float64(7419) || details["secure_port"] != float64(7443) {
		t.Fatalf("audit details = %#v", details)
	}
	if strings.Contains(string(queries.audit.Details), localAgentTestToken) || strings.Contains(string(queries.audit.Details), string(queries.agent.TokenSealed)) || strings.Contains(string(queries.audit.Details), string(ca)) {
		t.Fatal("audit included token, ciphertext, or CA PEM")
	}
}

func TestRegisterLocalAgentRefusesExistingRegistrationWithoutTLSOrAuditWrite(t *testing.T) {
	sealer, _, ca := localAgentTestMaterial(t)
	tenantID := uuid.New()
	queries := &fakeLocalAgentRegistrationQueries{insertRows: 0}
	committed := false
	svc := localAgentService(sealer, tenantID, queries, &committed)
	err := svc.RegisterLocalAgent(context.Background(), localAgentTestActor(tenantID), localAgentTestSessionHash, time.Hour, localAgentTestInput(uuid.New(), ca), localAgentTestToken)
	if !errors.Is(err, ErrAgentAlreadyProvisioned) {
		t.Fatalf("RegisterLocalAgent() error = %v, want existing-registration refusal", err)
	}
	if committed || queries.tls != nil || queries.audit != nil {
		t.Fatalf("existing agent wrote TLS/audit or committed: committed=%v tls=%#v audit=%#v", committed, queries.tls, queries.audit)
	}
}

func TestRegisterLocalAgentRejectsCrossTenantActorBeforeWrites(t *testing.T) {
	sealer, serverID, ca := localAgentTestMaterial(t)
	queries := &fakeLocalAgentRegistrationQueries{insertRows: 1}
	committed := false
	tenantID := uuid.New()
	otherTenantID := uuid.New()
	svc := localAgentService(sealer, tenantID, queries, &committed)
	err := svc.RegisterLocalAgent(context.Background(), localAgentTestActor(otherTenantID), localAgentTestSessionHash, time.Hour, localAgentTestInput(serverID, ca), localAgentTestToken)
	if err == nil {
		t.Fatal("cross-tenant actor was allowed to register an agent")
	}
	if committed || queries.agent != nil || queries.tls != nil || queries.audit != nil {
		t.Fatalf("cross-tenant registration wrote data: committed=%v agent=%#v tls=%#v audit=%#v", committed, queries.agent, queries.tls, queries.audit)
	}
}

func TestRegisterLocalAgentRejectsSessionChangedBeforeWrites(t *testing.T) {
	sealer, serverID, ca := localAgentTestMaterial(t)
	tenantID := uuid.New()
	actor := localAgentTestActor(tenantID)
	queries := &fakeLocalAgentRegistrationQueries{insertRows: 1}
	committed := false
	svc := &Service{
		Sealer:                       sealer,
		requireLocalAgentPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		localAgentTx: func(_ context.Context, txActor authz.Actor, txServerID uuid.UUID, _ []byte, _ time.Duration, fn func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error {
			staleSession := db.GetActorBySessionHashRow{ID: txActor.UserID, TenantID: txActor.TenantID, SessionID: uuid.New()}
			err := fn(db.GetServerRow{ID: txServerID, TenantID: tenantID, SiteID: uuid.New()}, staleSession, queries)
			committed = err == nil
			return err
		},
	}
	err := svc.RegisterLocalAgent(context.Background(), actor, localAgentTestSessionHash, time.Hour, localAgentTestInput(serverID, ca), localAgentTestToken)
	if err == nil {
		t.Fatal("registration proceeded with a session different from the authenticated actor")
	}
	if committed || queries.agent != nil || queries.tls != nil || queries.audit != nil {
		t.Fatalf("changed session wrote registration data: committed=%v agent=%#v tls=%#v audit=%#v", committed, queries.agent, queries.tls, queries.audit)
	}
}

func TestRegisterLocalAgentRevalidatesWithSameSessionHashAndIdleLimit(t *testing.T) {
	sealer, serverID, ca := localAgentTestMaterial(t)
	tenantID := uuid.New()
	actor := localAgentTestActor(tenantID)
	queries := &fakeLocalAgentRegistrationQueries{insertRows: 1}
	committed := false
	idle := 17 * time.Minute
	gotHash := []byte(nil)
	gotIdle := time.Duration(0)
	svc := &Service{
		Sealer:                       sealer,
		requireLocalAgentPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		localAgentTx: func(_ context.Context, txActor authz.Actor, txServerID uuid.UUID, sessionHash []byte, sessionIdle time.Duration, fn func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error {
			gotHash = append([]byte(nil), sessionHash...)
			gotIdle = sessionIdle
			session := db.GetActorBySessionHashRow{ID: txActor.UserID, TenantID: txActor.TenantID, SessionID: *txActor.SessionID}
			err := fn(db.GetServerRow{ID: txServerID, TenantID: tenantID, SiteID: uuid.New()}, session, queries)
			committed = err == nil
			return err
		},
	}
	if err := svc.RegisterLocalAgent(context.Background(), actor, localAgentTestSessionHash, idle, localAgentTestInput(serverID, ca), localAgentTestToken); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotHash, localAgentTestSessionHash) || gotIdle != idle || !committed {
		t.Fatalf("write-time session revalidation inputs = hash:%x idle:%s committed:%v", gotHash, gotIdle, committed)
	}
}

func TestRegisterLocalAgentRollsBackTLSOrAuditFailure(t *testing.T) {
	for _, failAt := range []string{"tls", "audit"} {
		t.Run(failAt, func(t *testing.T) {
			sealer, _, ca := localAgentTestMaterial(t)
			tenantID := uuid.New()
			queries := &fakeLocalAgentRegistrationQueries{insertRows: 1}
			failure := errors.New("database write failed")
			if failAt == "tls" {
				queries.tlsErr = failure
			} else {
				queries.auditErr = failure
			}
			committed := false
			svc := localAgentService(sealer, tenantID, queries, &committed)
			err := svc.RegisterLocalAgent(context.Background(), localAgentTestActor(tenantID), localAgentTestSessionHash, time.Hour, localAgentTestInput(uuid.New(), ca), localAgentTestToken)
			if !errors.Is(err, failure) {
				t.Fatalf("RegisterLocalAgent() error = %v, want write failure", err)
			}
			if committed {
				t.Fatal("partial registration transaction committed")
			}
		})
	}
}

func TestRegisterLocalAgentRejectsUnsafeInputsBeforeTransaction(t *testing.T) {
	sealer, _, ca := localAgentTestMaterial(t)
	cases := []struct {
		name   string
		mutate func(*LocalAgentRegistration, *string)
	}{
		{"DNS host", func(in *LocalAgentRegistration, _ *string) { in.Host = "agent.example" }},
		{"zero HTTP port", func(in *LocalAgentRegistration, _ *string) { in.HTTPPort = 0 }},
		{"same HTTP and HTTPS port", func(in *LocalAgentRegistration, _ *string) { in.HTTPPort = 7443 }},
		{"empty token", func(_ *LocalAgentRegistration, token *string) { *token = "\n " }},
		{"oversized token", func(_ *LocalAgentRegistration, token *string) {
			*token = strings.Repeat("x", maxLocalAgentTokenBytes+1)
		}},
		{"missing custom CA", func(in *LocalAgentRegistration, _ *string) { in.CAPEM = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := localAgentTestInput(uuid.New(), ca)
			token := "valid-transient-token"
			tc.mutate(&input, &token)
			transactions := 0
			tenantID := uuid.New()
			svc := &Service{
				Sealer:                       sealer,
				requireLocalAgentPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
				localAgentTx: func(context.Context, authz.Actor, uuid.UUID, []byte, time.Duration, func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error {
					transactions++
					return nil
				},
			}
			if err := svc.RegisterLocalAgent(context.Background(), localAgentTestActor(tenantID), localAgentTestSessionHash, time.Hour, input, token); err == nil {
				t.Fatal("unsafe registration input was accepted")
			}
			if transactions != 0 {
				t.Fatalf("transaction count = %d, want 0 for invalid input", transactions)
			}
		})
	}
}

func localAgentTestMaterial(t *testing.T) (*secrets.Sealer, uuid.UUID, []byte) {
	t.Helper()
	sealer, err := secrets.NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := testCA(t, "compose agent", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	return sealer, uuid.New(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
}

const localAgentTestToken = "0123456789012345678901234567890123456789012"

var localAgentTestSessionHash = identity.HashToken("test-active-session")

func localAgentTestActor(tenantID uuid.UUID) authz.Actor {
	sessionID := uuid.New()
	return authz.Actor{UserID: uuid.New(), Username: "operator", TenantID: &tenantID, SessionID: &sessionID}
}

func localAgentTestInput(serverID uuid.UUID, ca []byte) LocalAgentRegistration {
	return LocalAgentRegistration{ServerID: serverID, Host: "192.0.2.25", HTTPPort: 7419, SecurePort: 7443, CAPEM: ca}
}

func localAgentService(sealer *secrets.Sealer, tenantID uuid.UUID, q *fakeLocalAgentRegistrationQueries, committed *bool) *Service {
	return &Service{
		Sealer:                       sealer,
		requireLocalAgentPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		localAgentTx: func(_ context.Context, actor authz.Actor, serverID uuid.UUID, _ []byte, _ time.Duration, fn func(db.GetServerRow, db.GetActorBySessionHashRow, localAgentRegistrationQueries) error) error {
			err := fn(db.GetServerRow{ID: serverID, TenantID: tenantID, SiteID: uuid.New()}, db.GetActorBySessionHashRow{ID: actor.UserID, TenantID: actor.TenantID, SessionID: *actor.SessionID}, q)
			*committed = err == nil
			return err
		},
	}
}

type fakeLocalAgentRegistrationQueries struct {
	insertRows int64
	tlsErr     error
	auditErr   error
	agent      *db.RegisterServerAgentIfAbsentParams
	tls        *db.UpsertServerAgentTLSParams
	audit      *db.InsertAuditParams
}

func (q *fakeLocalAgentRegistrationQueries) RegisterServerAgentIfAbsent(_ context.Context, arg db.RegisterServerAgentIfAbsentParams) (int64, error) {
	q.agent = &arg
	return q.insertRows, nil
}

func (q *fakeLocalAgentRegistrationQueries) UpsertServerAgentTLS(_ context.Context, arg db.UpsertServerAgentTLSParams) error {
	q.tls = &arg
	return q.tlsErr
}

func (q *fakeLocalAgentRegistrationQueries) InsertAudit(_ context.Context, arg db.InsertAuditParams) error {
	q.audit = &arg
	return q.auditErr
}
