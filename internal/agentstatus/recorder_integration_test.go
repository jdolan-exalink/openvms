//go:build integration

package agentstatus_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type tenantServer struct{ tenant, server uuid.UUID }

func (ts tenantServer) id() agentca.Identity {
	return agentca.Identity{TenantID: ts.tenant, ServerID: ts.server}
}

func seed(t *testing.T, st *store.Store, name string) tenantServer {
	t.Helper()
	ctx := context.Background()
	ts := tenantServer{tenant: uuid.New(), server: uuid.New()}
	site := uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $2)", ts.tenant, name); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, $3)", site, ts.tenant, name); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, $4, 'http://x', 'u', '\\x00')",
			ts.server, ts.tenant, site, name)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

type row struct {
	LastSeen time.Time
	Version  string
	Status   string
}

// read returns the rows visible to scope.
func read(t *testing.T, st *store.Store, scope store.TenantScope) map[uuid.UUID]row {
	t.Helper()
	out := map[uuid.UUID]row{}
	err := st.TxRaw(context.Background(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), "SELECT server_id, last_seen_at, version, status FROM agent_heartbeats")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var r row
			if err := rows.Scan(&id, &r.LastSeen, &r.Version, &r.Status); err != nil {
				return err
			}
			out[id] = r
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordHeartbeatUpsertsTheServersRow(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	a := seed(t, st, "a")
	rec := &agentstatus.Recorder{Store: st}
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	if err := rec.RecordHeartbeat(ctx, a.id(), agentstatus.Heartbeat{Version: "0.1.1", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	first := read(t, st, store.AllTenants)[a.server]
	if first.Version != "0.1.1" || first.Status != "online" || first.LastSeen.Before(before) {
		t.Fatalf("row = %+v, want version 0.1.1 online seen after %v", first, before)
	}

	time.Sleep(20 * time.Millisecond)
	if err := rec.RecordHeartbeat(ctx, a.id(), agentstatus.Heartbeat{Version: "0.2.0", Status: "degraded"}); err != nil {
		t.Fatal(err)
	}
	rows := read(t, st, store.AllTenants)
	if len(rows) != 1 {
		t.Fatalf("%d rows after two heartbeats, want 1 per server", len(rows))
	}
	second := rows[a.server]
	if second.Version != "0.2.0" || second.Status != "degraded" || !second.LastSeen.After(first.LastSeen) {
		t.Fatalf("row = %+v, want version 0.2.0 degraded and a later last_seen_at than %v", second, first.LastSeen)
	}
}

func TestRecordHeartbeatIsTenantIsolated(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	a, b := seed(t, st, "a"), seed(t, st, "b")
	rec := &agentstatus.Recorder{Store: st}
	ctx := context.Background()

	if err := rec.RecordHeartbeat(ctx, a.id(), agentstatus.Heartbeat{Version: "1", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordHeartbeat(ctx, b.id(), agentstatus.Heartbeat{Version: "2", Status: "offline"}); err != nil {
		t.Fatal(err)
	}

	// Each tenant sees only its own server's row.
	for _, tc := range []struct {
		name         string
		own, foreign tenantServer
		version      string
	}{{"a", a, b, "1"}, {"b", b, a, "2"}} {
		rows := read(t, st, store.TenantScope{TenantID: tc.own.tenant})
		if len(rows) != 1 || rows[tc.own.server].Version != tc.version {
			t.Errorf("tenant %s sees %+v, want only its own row (version %s)", tc.name, rows, tc.version)
		}
		if _, leaked := rows[tc.foreign.server]; leaked {
			t.Errorf("tenant %s sees the other tenant's heartbeat", tc.name)
		}
	}

	// An identity that pairs one tenant with another tenant's server writes nothing.
	crossed := agentca.Identity{TenantID: a.tenant, ServerID: b.server}
	if err := rec.RecordHeartbeat(ctx, crossed, agentstatus.Heartbeat{Version: "evil", Status: "online"}); err == nil {
		t.Fatal("recorded a heartbeat for another tenant's server")
	}
	if got := read(t, st, store.AllTenants)[b.server]; got.Version != "2" {
		t.Fatalf("tenant b's row = %+v, was changed by tenant a", got)
	}
}

func TestRecordHeartbeatRejectsAnUnknownStatus(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	a := seed(t, st, "a")
	rec := &agentstatus.Recorder{Store: st}
	if err := rec.RecordHeartbeat(context.Background(), a.id(), agentstatus.Heartbeat{Status: "on fire"}); err == nil {
		t.Fatal("accepted an unknown status")
	}
}
