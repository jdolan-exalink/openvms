//go:build integration

package events

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestUpsertPlateDeduplicationWithinWindow(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	tenantID := uuid.New()
	siteID := uuid.New()
	serverID := uuid.New()
	cameraID := uuid.New()
	ctx := context.Background()

	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{tenantID}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{siteID, tenantID}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'srv', 'http://example.invalid')`, []any{serverID, tenantID, siteID}},
			{`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name, lpr) VALUES ($1, $2, $3, $4, 'cam-a', 'Cam A', true)`, []any{cameraID, tenantID, siteID, serverID}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := db.FrigateServer{ID: serverID, TenantID: tenantID, SiteID: siteID}
	baseTime := float64(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC).Unix())

	// 1. First Frigate detection: score 0.80 at t=0
	score1 := 0.80
	obj1 := frigate.TrackedObject{
		ID:         "event-1",
		Plate:      "AB 123 CD",
		PlateScore: &score1,
		StartTime:  baseTime,
		Label:      "car",
	}

	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return upsertPlate(ctx, tx, srv, cameraID, obj1)
	})
	if err != nil {
		t.Fatalf("upsertPlate obj1: %v", err)
	}

	// 2. Second Frigate detection: same plate 3 seconds later, higher score 0.95
	score2 := 0.95
	obj2 := frigate.TrackedObject{
		ID:         "event-2",
		Plate:      "AB123CD",
		PlateScore: &score2,
		StartTime:  baseTime + 3,
		Label:      "car",
	}

	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return upsertPlate(ctx, tx, srv, cameraID, obj2)
	})
	if err != nil {
		t.Fatalf("upsertPlate obj2: %v", err)
	}

	// Verify deduplication: should still be exactly 1 row in lpr_reads with score 0.95
	var count int
	var score float32
	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), coalesce(max(score), 0) FROM lpr_reads WHERE camera_id = $1 AND plate_normalized = 'AB123CD'`, cameraID).Scan(&count, &score)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("lpr_reads count within window = %d, want 1", count)
	}
	if score < 0.94 {
		t.Fatalf("lpr_reads score after dedup update = %f, want ~0.95", score)
	}

	// 3. Third Frigate detection: 40 seconds later (outside 20s window) -> new row
	score3 := 0.88
	obj3 := frigate.TrackedObject{
		ID:         "event-3",
		Plate:      "AB123CD",
		PlateScore: &score3,
		StartTime:  baseTime + 40,
		Label:      "car",
	}

	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return upsertPlate(ctx, tx, srv, cameraID, obj3)
	})
	if err != nil {
		t.Fatalf("upsertPlate obj3: %v", err)
	}

	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM lpr_reads WHERE camera_id = $1 AND plate_normalized = 'AB123CD'`, cameraID).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("lpr_reads count after outside window = %d, want 2", count)
	}
}

type dedupFakeAgent struct {
	epoch   string
	lastSeq uint64
	queue   []provision.LPRQueuedEvent
}

func (a *dedupFakeAgent) PullProvisionLPREvents(_ context.Context, _ uuid.UUID, after uint64) (provision.LPREventsPage, error) {
	page := provision.LPREventsPage{LastSeq: a.lastSeq, Depth: len(a.queue), Epoch: a.epoch}
	for _, e := range a.queue {
		if e.Seq > after {
			page.Events = append(page.Events, e)
		}
	}
	return page, nil
}

func (a *dedupFakeAgent) AckProvisionLPREvents(_ context.Context, _ uuid.UUID, through uint64) error {
	return nil
}

func TestAgentIngestDeduplicationWithExistingRead(t *testing.T) {
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	tenantID := uuid.New()
	siteID := uuid.New()
	serverID := uuid.New()
	cameraID := uuid.New()
	ctx := context.Background()

	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{tenantID}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{siteID, tenantID}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'srv', 'http://example.invalid')`, []any{serverID, tenantID, siteID}},
			{`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name, lpr) VALUES ($1, $2, $3, $4, 'cam-a', 'Cam A', true)`, []any{cameraID, tenantID, siteID, serverID}},
			{`INSERT INTO server_agents (server_id, tenant_id, host, port, token_sealed) VALUES ($1, $2, '192.0.2.10', 7419, '\x00')`, []any{serverID, tenantID}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := db.FrigateServer{ID: serverID, TenantID: tenantID, SiteID: siteID}
	baseTime := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

	// 1. Frigate detection inserts initial lpr_read without crop_key
	score1 := 0.85
	obj1 := frigate.TrackedObject{
		ID:         "frigate-event-100",
		Plate:      "XY987ZT",
		PlateScore: &score1,
		StartTime:  float64(baseTime.Unix()),
		Label:      "car",
	}
	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return upsertPlate(ctx, tx, srv, cameraID, obj1)
	})
	if err != nil {
		t.Fatalf("upsertPlate: %v", err)
	}

	// 2. Agent ingest receives camera reading 2 seconds later with plate crop
	agent := &dedupFakeAgent{
		epoch: "epoch-1",
		lastSeq: 1,
		queue: []provision.LPRQueuedEvent{
			{
				Seq: 1,
				ID:  "agent-event-1",
				Event: provision.LPRAgentEvent{
					CameraID:     cameraID.String(),
					Plate:        "XY 987 ZT",
					EventTime:    baseTime.Add(2 * time.Second),
					ReceivedTime: baseTime.Add(2 * time.Second),
					EventType:    "lpr",
				},
			},
		},
	}
	ing := &AgentIngestor{Store: st, Source: agent, Log: pgtest.Discard(), Interval: time.Hour}
	if err := ing.IngestServer(ctx, serverID, tenantID); err != nil {
		t.Fatalf("IngestServer: %v", err)
	}

	// Verify that the agent reading merged into the existing row instead of creating a second row
	var count int
	var cropAttempts int
	err = st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), max(crop_attempts) FROM lpr_reads WHERE camera_id = $1 AND plate_normalized = 'XY987ZT'`, cameraID).Scan(&count, &cropAttempts)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("lpr_reads count after agent merge = %d, want 1", count)
	}
}
