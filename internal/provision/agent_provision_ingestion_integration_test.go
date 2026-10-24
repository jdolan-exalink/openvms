//go:build integration

package provision

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestGetProvisionLPRIngestionReadsTheServersCursorAndOnlyThatServers(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	ctx := context.Background()
	tenant, site, server, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pulled := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{tenant}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{site, tenant}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'a', 'http://example.invalid')`, []any{server, tenant, site}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'b', 'http://example.invalid')`, []any{other, tenant, site}},
			{`INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_seq, ingested_total, skipped_total, agent_lost, agent_depth, last_pull_at, last_error)
			  VALUES ($1, $2, 42, 40, 2, 3, 5, $3, 'agent returned 401')`, []any{server, tenant, pulled}},
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
	svc := &Service{Store: st, requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error { return nil }}
	platform := authz.Actor{System: true}

	got, err := svc.GetProvisionLPRIngestion(ctx, platform, server)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Known || got.LastSeq != 42 || got.IngestedTotal != 40 || got.SkippedTotal != 2 || got.AgentLost != 3 || got.AgentDepth != 5 ||
		got.LastError != "agent returned 401" || got.LastPullAt == nil || !got.LastPullAt.Equal(pulled) {
		t.Fatalf("ingestion = %+v", got)
	}
	never, err := svc.GetProvisionLPRIngestion(ctx, platform, other)
	if err != nil {
		t.Fatal(err)
	}
	if never.Known || never.IngestedTotal != 0 {
		t.Fatalf("a server that was never pulled must read as unknown, got %+v", never)
	}
}
