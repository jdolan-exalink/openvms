//go:build integration

package provision

import (
	"context"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestConnectAgentStoresTheRegistrationOnlyAfterTheAgentAnswers(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	sealer, err := secrets.NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	tenant, site, server, sshServer := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor := authz.Actor{System: true}
	if err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{tenant}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{site, tenant}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'remote', 'http://example.invalid')`, []any{server, tenant, site}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'ssh', 'http://example.invalid')`, []any{sshServer, tenant, site}},
			{`INSERT INTO server_agents (server_id, tenant_id, host, port, variant, token_sealed, version) VALUES ($1, $2, '192.0.2.44', 7419, 'ssh', '\x00', '0.3.0')`, []any{sshServer, tenant}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	paths := map[string]int{"/v1/metrics": 200, "/v1/provision-lpr/events/after/0": 200, "/v1/provision-lpr/registry": 403}
	svc := &Service{Store: st, Sealer: sealer,
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		requireServerManage:            func(context.Context, authz.Actor, uuid.UUID) error { return nil }}
	svc.probeRoundTripper = agentAnswering("agent-token", paths)

	// A wrong token stores nothing.
	if _, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "http://10.8.0.5:7419", Token: "wrong"}); !errors.Is(err, ErrAgentConnectUnauthorized) {
		t.Fatalf("wrong token = %v", err)
	}
	if c, _ := svc.GetAgentConnection(ctx, actor, server); c.Connected {
		t.Fatal("a refused connection left a registration")
	}

	res, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "http://10.8.0.5:7419", Token: "agent-token", Headers: map[string]string{"CF-Access-Client-Id": "id"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "0.4.0" || res.BaseURL != "http://10.8.0.5:7419" || !res.Scopes.Read || !res.Scopes.Events || res.Scopes.Control {
		t.Fatalf("result = %+v", res)
	}
	var row db.ServerAgent
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgent(ctx, server)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	opened, err := sealer.Open(row.TokenSealed, server[:])
	if err != nil || string(opened) != "agent-token" || row.BaseUrl != "http://10.8.0.5:7419" || row.Variant != "remote" || row.Version != "0.4.0" {
		t.Fatalf("row = %+v (token %q, %v)", row, opened, err)
	}
	if string(row.TokenSealed) == "agent-token" {
		t.Fatal("the token is stored in the clear")
	}
	if conn, _ := svc.GetAgentConnection(ctx, actor, server); !conn.Connected || !conn.HasHeaders || conn.HasCustomCA || conn.BaseURL != "http://10.8.0.5:7419" {
		t.Fatalf("connection = %+v", conn)
	}
	if _, headers, err := svc.agentConnection(ctx, actor, server); err != nil || headers["CF-Access-Client-Id"] != "id" {
		t.Fatalf("headers = %v, %v", headers, err)
	}

	// A second connect needs the explicit replace, and a failed one leaves the working one in place.
	if _, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "http://10.8.0.6:7419", Token: "agent-token"}); !errors.Is(err, ErrAgentAlreadyProvisioned) {
		t.Fatalf("second connect without replace = %v", err)
	}
	if _, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "http://10.8.0.6:7419", Token: "nope", Replace: true}); !errors.Is(err, ErrAgentConnectUnauthorized) {
		t.Fatalf("replace with a bad token = %v", err)
	}
	if conn, _ := svc.GetAgentConnection(ctx, actor, server); conn.BaseURL != "http://10.8.0.5:7419" {
		t.Fatalf("a failed replacement changed the registration: %+v", conn)
	}

	// A private CA is stored with the address it was given for, and a replacement does not inherit it.
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}))
	if _, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "https://agent.example.com", Token: "agent-token", CAPEM: ca, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if conn, _ := svc.GetAgentConnection(ctx, actor, server); !conn.HasCustomCA || conn.HasHeaders {
		t.Fatalf("connection with a CA = %+v", conn)
	}
	if _, err := svc.ConnectAgent(ctx, actor, server, ConnectAgentInput{URL: "http://10.8.0.5:7419", Token: "agent-token", Replace: true}); err != nil {
		t.Fatal(err)
	}
	if conn, _ := svc.GetAgentConnection(ctx, actor, server); conn.HasCustomCA {
		t.Fatalf("a replaced agent inherited the previous certificate authority: %+v", conn)
	}

	// Disconnect forgets a connected agent but never one installed over SSH.
	if err := svc.DisconnectAgent(ctx, actor, sshServer); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disconnecting an SSH-installed agent = %v, want not found", err)
	}
	if err := svc.DisconnectAgent(ctx, actor, server); err != nil {
		t.Fatal(err)
	}
	if conn, _ := svc.GetAgentConnection(ctx, actor, server); conn.Connected {
		t.Fatal("still connected after disconnect")
	}
	if _, err := svc.agentRow(ctx, actor, sshServer); err != nil {
		t.Fatalf("the SSH-installed registration was touched: %v", err)
	}

	// Replacing an SSH-installed registration is allowed, but only with the explicit confirmation,
	// like replacing any other registration: the old agent stops being followed by this OpenVMS.
	if _, err := svc.ConnectAgent(ctx, actor, sshServer, ConnectAgentInput{URL: "http://10.8.0.5:7419", Token: "agent-token"}); !errors.Is(err, ErrAgentAlreadyProvisioned) {
		t.Fatalf("connecting over an SSH install without confirmation = %v", err)
	}
	if row, _ := svc.agentRow(ctx, actor, sshServer); row.Variant != "ssh" || row.BaseUrl != "" {
		t.Fatalf("a refused replacement changed the SSH registration: %+v", row)
	}
	if _, err := svc.ConnectAgent(ctx, actor, sshServer, ConnectAgentInput{URL: "http://10.8.0.5:7419", Token: "agent-token", Replace: true}); err != nil {
		t.Fatal(err)
	}
	if row, _ := svc.agentRow(ctx, actor, sshServer); row.Variant != "remote" || row.BaseUrl != "http://10.8.0.5:7419" {
		t.Fatalf("the confirmed replacement was not stored: %+v", row)
	}
}
