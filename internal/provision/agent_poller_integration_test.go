//go:build integration

package provision

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestAgentPollerAsksOnlyTheAgentsWithoutAFreshHeartbeatAndRecordsThem(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	ctx := context.Background()
	tenant, site := uuid.New(), uuid.New()
	never, fresh, stale, deleted := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, site, tenant); err != nil {
			return err
		}
		for i, id := range []uuid.UUID{never, fresh, stale, deleted} {
			if _, err := tx.Exec(ctx, `INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, $4, 'http://example.invalid')`, id, tenant, site, "s"+string(rune('a'+i))); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO server_agents (server_id, tenant_id, host, port, token_sealed) VALUES ($1, $2, '192.0.2.10', 7419, '\x00')`, id, tenant); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE frigate_servers SET deleted_at = now() WHERE id = $1`, deleted); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_heartbeats (server_id, tenant_id, last_seen_at, version, status) VALUES ($1, $2, now(), '0.3.0', 'online')`, fresh, tenant); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agent_heartbeats (server_id, tenant_id, last_seen_at, version, status) VALUES ($1, $2, now() - interval '1 hour', '0.3.0', 'online')`, stale, tenant)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	asked := map[uuid.UUID]bool{}
	p := &AgentPoller{
		Store: st, Recorder: &agentstatus.Recorder{Store: st},
		fetch: func(_ context.Context, id uuid.UUID) (agent.Snapshot, error) {
			mu.Lock()
			asked[id] = true
			mu.Unlock()
			return agent.Snapshot{Version: "0.4.0"}, nil
		},
	}
	p.PollAll(ctx)

	if !asked[never] || !asked[stale] || asked[fresh] || asked[deleted] {
		t.Fatalf("asked = %v; want the agent that never reported and the stale one, not the fresh one nor the deleted server", asked)
	}
	read := func(id uuid.UUID) (version, status string, recent bool) {
		if err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT version, status, last_seen_at > now() - interval '1 minute' FROM agent_heartbeats WHERE server_id = $1`, id).Scan(&version, &status, &recent)
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, id := range []uuid.UUID{never, stale} {
		if v, s, recent := read(id); v != "0.4.0" || s != agentstatus.Online || !recent {
			t.Errorf("server %s: version=%q status=%q recent=%v", id, v, s, recent)
		}
	}
	if v, _, _ := read(fresh); v != "0.3.0" {
		t.Errorf("the fresh heartbeat was overwritten: version %q", v)
	}
}
