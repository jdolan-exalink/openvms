package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type fakeAgentUpdateTxQueries struct {
	server       db.GetServerForUpdateRow
	agent        db.ServerAgent
	tls          db.ServerAgentTl
	insertRows   int64
	insertCalls  int
	auditCalls   int
	serverReads  int
	agentReads   int
	insertParams db.RegisterServerAgentTLSIfAbsentParams
	auditParams  db.InsertAuditParams
}

func (q *fakeAgentUpdateTxQueries) GetServerForUpdate(context.Context, uuid.UUID) (db.GetServerForUpdateRow, error) {
	q.serverReads++
	return q.server, nil
}
func (q *fakeAgentUpdateTxQueries) GetServerAgentForUpdate(context.Context, uuid.UUID) (db.ServerAgent, error) {
	q.agentReads++
	return q.agent, nil
}
func (q *fakeAgentUpdateTxQueries) GetServerAgentTLS(context.Context, uuid.UUID) (db.ServerAgentTl, error) {
	return q.tls, nil
}
func (q *fakeAgentUpdateTxQueries) RegisterServerAgentTLSIfAbsent(_ context.Context, params db.RegisterServerAgentTLSIfAbsentParams) (int64, error) {
	q.insertCalls++
	q.insertParams = params
	return q.insertRows, nil
}
func (q *fakeAgentUpdateTxQueries) InsertAudit(_ context.Context, params db.InsertAuditParams) error {
	q.auditCalls++
	q.auditParams = params
	return nil
}

type fakeAgentUpdateTxChecker struct {
	denied authz.Permission
	calls  []authz.Permission
}

func (c *fakeAgentUpdateTxChecker) Require(permission authz.Permission, _ authz.Resource) error {
	c.calls = append(c.calls, permission)
	if permission == c.denied {
		return errors.New("permission revoked")
	}
	return nil
}

func TestPersistUpdatedAgentTLSInTransactionRejectsChangedRegistrationBinding(t *testing.T) {
	baseServerID, tenantID, siteID := uuid.New(), uuid.New(), uuid.New()
	original := db.ServerAgent{ServerID: baseServerID, TenantID: tenantID, Host: "192.0.2.44", Port: 7419, TokenSealed: []byte("sealed-original")}
	server := db.GetServerForUpdateRow{ID: baseServerID, TenantID: tenantID, SiteID: siteID}
	for _, tc := range []struct {
		name   string
		mutate func(*db.ServerAgent)
	}{
		{"bearer rotated", func(row *db.ServerAgent) { row.TokenSealed = []byte("sealed-rotated") }},
		{"host changed", func(row *db.ServerAgent) { row.Host = "192.0.2.45" }},
		{"port changed", func(row *db.ServerAgent) { row.Port = 7420 }},
		{"tenant changed", func(row *db.ServerAgent) { row.TenantID = uuid.New() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := original
			tc.mutate(&current)
			queries := &fakeAgentUpdateTxQueries{server: server, agent: current, insertRows: 1}
			checker := &fakeAgentUpdateTxChecker{}
			err := persistAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom, CAPEM: []byte("public CA")})
			if err == nil || queries.serverReads != 1 || queries.agentReads != 1 || queries.insertCalls != 0 || queries.auditCalls != 0 {
				t.Fatalf("err=%v inserts=%d audits=%d", err, queries.insertCalls, queries.auditCalls)
			}
		})
	}
}

func TestPersistUpdatedAgentTLSInTransactionRechecksBothPermissionsAndUsesInsertOnly(t *testing.T) {
	serverID, tenantID := uuid.New(), uuid.New()
	original := db.ServerAgent{ServerID: serverID, TenantID: tenantID, Host: "192.0.2.44", Port: 7419, TokenSealed: []byte("sealed-original")}
	ca, _ := newAgentInstallTLS(t, original.Host)
	config := AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom, CAPEM: ca}
	queries := &fakeAgentUpdateTxQueries{server: db.GetServerForUpdateRow{ID: serverID, TenantID: tenantID, SiteID: uuid.New()}, agent: original, insertRows: 1}
	checker := &fakeAgentUpdateTxChecker{denied: authz.ServersConfigSecrets}
	err := persistAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, config)
	if err == nil || queries.serverReads != 1 || queries.agentReads != 0 || queries.insertCalls != 0 || queries.auditCalls != 0 || len(checker.calls) != 2 || checker.calls[0] != authz.ServersManage || checker.calls[1] != authz.ServersConfigSecrets {
		t.Fatalf("revoked permission did not stop persistence: err=%v calls=%v inserts=%d", err, checker.calls, queries.insertCalls)
	}

	checker = &fakeAgentUpdateTxChecker{}
	queries = &fakeAgentUpdateTxQueries{server: db.GetServerForUpdateRow{ID: serverID, TenantID: tenantID, SiteID: uuid.New()}, agent: original, insertRows: 0}
	err = persistAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, config)
	if !errors.Is(err, ErrAgentUpdateTLSAlreadyConfigured) || queries.insertCalls != 1 || queries.auditCalls != 0 {
		t.Fatalf("concurrent trust creation was not refused: err=%v inserts=%d audits=%d", err, queries.insertCalls, queries.auditCalls)
	}
	queries.insertRows = 1
	err = persistAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, config)
	if err != nil || queries.auditCalls != 1 || queries.insertParams.ServerID != serverID || queries.insertParams.TenantID != tenantID || queries.insertParams.SecurePort != 7443 || queries.insertParams.CaPem == nil || *queries.insertParams.CaPem != string(ca) {
		t.Fatalf("successful transaction persistence err=%v params=%+v audits=%d", err, queries.insertParams, queries.auditCalls)
	}
	if string(queries.auditParams.Details) == string(ca) || len(queries.auditParams.Details) == 0 {
		t.Fatal("audit did not use redacted details")
	}
}

func TestVerifyAgentUpdateTLSInTransactionRequiresUnchangedTrustBindingAndPermissions(t *testing.T) {
	serverID, tenantID := uuid.New(), uuid.New()
	original := db.ServerAgent{ServerID: serverID, TenantID: tenantID, Host: "192.0.2.44", Port: 7419, TokenSealed: []byte("sealed-original")}
	ca, _ := newAgentInstallTLS(t, original.Host)
	expected := AgentTLSConfig{SecurePort: 7443, TrustMode: AgentTLSTrustCustom, CAPEM: ca}
	makeQueries := func(config AgentTLSConfig) *fakeAgentUpdateTxQueries {
		return &fakeAgentUpdateTxQueries{
			server: db.GetServerForUpdateRow{ID: serverID, TenantID: tenantID, SiteID: uuid.New()},
			agent:  original,
			tls:    db.ServerAgentTl{ServerID: serverID, TenantID: tenantID, SecurePort: int32(config.SecurePort), TrustMode: config.TrustMode, CaPem: nullCA(config)},
		}
	}
	changed := cloneAgentTLSConfig(expected)
	changed.SecurePort++
	for name, config := range map[string]AgentTLSConfig{"port changed": changed, "trust changed": {SecurePort: expected.SecurePort, TrustMode: expected.TrustMode, CAPEM: []byte("different public trust")}} {
		t.Run(name, func(t *testing.T) {
			queries := makeQueries(config)
			checker := &fakeAgentUpdateTxChecker{}
			err := verifyAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, expected)
			if err == nil || queries.auditCalls != 0 || queries.insertCalls != 0 {
				t.Fatalf("changed trust was accepted or audited: err=%v audits=%d", err, queries.auditCalls)
			}
		})
	}
	queries := makeQueries(expected)
	checker := &fakeAgentUpdateTxChecker{denied: authz.ServersConfigSecrets}
	err := verifyAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, expected)
	if err == nil || queries.agentReads != 0 || queries.auditCalls != 0 || len(checker.calls) != 2 {
		t.Fatalf("revoked permission did not stop verification: err=%v reads=%d audits=%d permissions=%v", err, queries.agentReads, queries.auditCalls, checker.calls)
	}
	queries = makeQueries(expected)
	checker = &fakeAgentUpdateTxChecker{}
	queries.agent.TokenSealed = []byte("rotated-token")
	err = verifyAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, expected)
	if err == nil || queries.auditCalls != 0 {
		t.Fatalf("changed bearer binding was accepted or audited: err=%v audits=%d", err, queries.auditCalls)
	}
	queries = makeQueries(expected)
	checker = &fakeAgentUpdateTxChecker{}
	err = verifyAgentUpdateTLSInTransaction(context.Background(), queries, func(context.Context, db.GetServerForUpdateRow) (agentUpdateTxChecker, error) { return checker, nil }, authz.Actor{UserID: uuid.New()}, original, expected)
	if err != nil || queries.agentReads != 1 || queries.auditCalls != 1 || queries.auditParams.Action != "SERVER_AGENT_BINARY_UPDATED_TLS_PRESERVED" {
		t.Fatalf("unchanged trust verification err=%v reads=%d audits=%d action=%q", err, queries.agentReads, queries.auditCalls, queries.auditParams.Action)
	}
	if strings.Contains(string(queries.auditParams.Details), string(ca)) || strings.Contains(string(queries.auditParams.Details), string(original.TokenSealed)) {
		t.Fatal("trust-preservation audit leaked public CA or sealed token bytes")
	}
}

var _ agentUpdateTxChecker = (*access.Checker)(nil)
