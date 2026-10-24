//go:build integration

package provision

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestSetAgentRequestHeadersStoresThemSealedAndTheClientReadsThemBack(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	sealer, err := secrets.NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tenant, site, server := uuid.New(), uuid.New(), uuid.New()
	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{tenant}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{site, tenant}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'a', 'http://example.invalid')`, []any{server, tenant, site}},
			{`INSERT INTO server_agents (server_id, tenant_id, host, port, token_sealed, base_url) VALUES ($1, $2, 'agent.example.com', 443, '\x00', 'https://agent.example.com')`, []any{server, tenant}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: st, Sealer: sealer, requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error { return nil }}
	platform := authz.Actor{System: true}

	if err := svc.SetAgentRequestHeaders(ctx, platform, server, map[string]string{"CF-Access-Client-Id": "id", "CF-Access-Client-Secret": "super-secret"}); err != nil {
		t.Fatal(err)
	}
	base, headers, err := svc.agentConnection(ctx, platform, server)
	if err != nil || base != "https://agent.example.com" || headers["CF-Access-Client-Secret"] != "super-secret" {
		t.Fatalf("connection = %q %v %v", base, headers, err)
	}
	var raw []byte
	if err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT request_headers_sealed FROM server_agents WHERE server_id = $1`, server).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte("super-secret")) {
		t.Fatalf("the stored value must be sealed, got %q", raw)
	}

	// Refused values never reach the database, and an agent that does not exist is reported.
	if err := svc.SetAgentRequestHeaders(ctx, platform, server, map[string]string{"Authorization": "Bearer x"}); err == nil {
		t.Fatal("a reserved header must be refused")
	}
	if err := svc.SetAgentRequestHeaders(ctx, platform, uuid.New(), map[string]string{"X-A": "b"}); !errors.Is(err, ErrAgentUnprovisioned) {
		t.Fatalf("unknown agent err = %v", err)
	}
	// An empty map clears them.
	if err := svc.SetAgentRequestHeaders(ctx, platform, server, nil); err != nil {
		t.Fatal(err)
	}
	if _, headers, _ := svc.agentConnection(ctx, platform, server); len(headers) != 0 {
		t.Fatalf("headers after clearing = %v", headers)
	}
}
