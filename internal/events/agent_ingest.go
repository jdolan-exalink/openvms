package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// AgentEventSource is an edge agent's durable queue of camera-reported plate readings, as central
// reads it: a page of events after a sequence, and an acknowledgement of what was stored.
// provision.Service implements it.
type AgentEventSource interface {
	PullProvisionLPREvents(ctx context.Context, serverID uuid.UUID, after uint64) (provision.LPREventsPage, error)
	AckProvisionLPREvents(ctx context.Context, serverID uuid.UUID, through uint64) error
}

const (
	// agentReadPrefix marks the lpr_reads of this source inside (server_id, remote_event_id), so they
	// can never collide with a Frigate event ID.
	agentReadPrefix = "agent:"
	// agentReadLabel is the label stored with a camera-reported plate.
	agentReadLabel = "license_plate"
	// A camera-reported reading has no crop in Frigate: the copier skips a read that has used up its
	// attempts, so it is stored as already tried.
	agentReadCropAttempts = maxThumbAttempts

	agentIngestMaxPages    = 20
	agentIngestServerLimit = time.Minute
)

// AgentIngestor pulls the plate readings Provision-ISR cameras report to their edge agent and stores
// them in lpr_reads, so they are searchable next to the readings Frigate produced.
//
// It is safe to run in several replicas and to repeat: a reading is keyed by the ID the agent gave it,
// so storing it again changes nothing, and the cursor only moves in the same transaction that stores
// the page. The agent keeps what was not acknowledged.
type AgentIngestor struct {
	Store       *store.Store
	Source      AgentEventSource
	Log         *slog.Logger
	Interval    time.Duration
	Concurrency int
}

// Run ingests every agent each Interval until ctx ends.
func (a *AgentIngestor) Run(ctx context.Context) {
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	for {
		a.IngestAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type agentTarget struct {
	serverID, tenantID uuid.UUID
}

// IngestAll pulls every server that has an edge agent once.
func (a *AgentIngestor) IngestAll(ctx context.Context) {
	var targets []agentTarget
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT sa.server_id, sa.tenant_id
FROM server_agents sa JOIN frigate_servers fs ON fs.id = sa.server_id
WHERE fs.deleted_at IS NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t agentTarget
			if err := rows.Scan(&t.serverID, &t.tenantID); err != nil {
				return err
			}
			targets = append(targets, t)
		}
		return rows.Err()
	})
	if err != nil {
		a.Log.ErrorContext(ctx, "agent ingest: list agents", "error", err)
		return
	}
	sem := make(chan struct{}, max(a.Concurrency, 4))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			cctx, cancel := context.WithTimeout(ctx, agentIngestServerLimit)
			defer cancel()
			if err := a.IngestServer(cctx, target.serverID, target.tenantID); err != nil && ctx.Err() == nil {
				a.Log.WarnContext(ctx, "agent ingest: server", "server_id", target.serverID, "error", err)
			}
		}()
	}
	wg.Wait()
}

// IngestServer drains one agent's queue, a page at a time, and records the outcome.
func (a *AgentIngestor) IngestServer(ctx context.Context, serverID, tenantID uuid.UUID) error {
	cursor, err := a.loadCursor(ctx, serverID)
	if err != nil {
		return err
	}
	reset := false
	for range agentIngestMaxPages {
		page, err := a.Source.PullProvisionLPREvents(ctx, serverID, cursor)
		if err != nil {
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
		if page.LastSeq < cursor && !reset {
			// The agent starts again from 1 after its state is wiped; events below the old cursor would
			// otherwise never be read.
			a.Log.WarnContext(ctx, "agent ingest: the agent's sequence went backwards; reading from the start",
				"server_id", serverID, "cursor", cursor, "agent_last_seq", page.LastSeq)
			cursor, reset = 0, true
			if err := a.saveCursorOnly(ctx, serverID, tenantID, 0); err != nil {
				return err
			}
			continue
		}
		if len(page.Events) == 0 {
			if err := a.touch(ctx, serverID, tenantID, page); err != nil {
				return err
			}
			if page.Depth > 0 && cursor > 0 {
				// A previous acknowledgement failed: everything up to the cursor is stored already.
				if err := a.Source.AckProvisionLPREvents(ctx, serverID, cursor); err != nil {
					a.recordError(ctx, serverID, tenantID, err)
					return err
				}
			}
			return nil
		}
		through, err := a.storePage(ctx, serverID, tenantID, cursor, page)
		if err != nil {
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
		cursor = through
		if err := a.Source.AckProvisionLPREvents(ctx, serverID, cursor); err != nil {
			// Stored and the cursor moved: the agent only keeps the events a little longer, and the
			// next run acknowledges them.
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
	}
	return nil
}

func (a *AgentIngestor) loadCursor(ctx context.Context, serverID uuid.UUID) (uint64, error) {
	var seq int64
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT last_seq FROM server_agent_event_cursors WHERE server_id = $1`, serverID).Scan(&seq)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("load agent cursor: %w", err)
	}
	return uint64(max(seq, 0)), nil //nolint:gosec // clamped non-negative above
}

// storePage stores the valid readings of a page and moves the cursor, in one transaction. It returns
// the highest sequence handled, which is also what the agent is told to drop. Readings central cannot
// attribute are counted and left behind: holding the cursor for them would block every later reading.
func (a *AgentIngestor) storePage(ctx context.Context, serverID, tenantID uuid.UUID, cursor uint64, page provision.LPREventsPage) (uint64, error) {
	through := cursor
	var ingested, skipped int64
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		ingested, skipped = 0, 0
		through = cursor
		sites, err := serverCameras(ctx, tx, serverID, tenantID)
		if err != nil {
			return err
		}
		for _, e := range page.Events {
			if e.Seq <= cursor {
				continue
			}
			through = max(through, e.Seq)
			cam, plate := uuid.Nil, NormalizePlate(e.Event.Plate)
			if id, err := uuid.Parse(e.Event.CameraID); err == nil {
				cam = id
			}
			siteID, known := sites[cam]
			if !known || plate == "" || e.ID == "" {
				skipped++
				continue
			}
			var score *float32
			if e.Event.Confidence != nil {
				v := float32(*e.Event.Confidence)
				score = &v
			}
			seen := e.Event.EventTime
			if seen.IsZero() {
				seen = e.Event.ReceivedTime
			}
			tag, err := tx.Exec(ctx, `
INSERT INTO lpr_reads (tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, score, label, sub_label, zones, seen_at, crop_attempts)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', '{}', $10, $11)
ON CONFLICT (server_id, remote_event_id) DO NOTHING`,
				tenantID, siteID, serverID, cam, agentReadPrefix+e.ID, e.Event.Plate, plate, score, agentReadLabel, seen.UTC(), agentReadCropAttempts)
			if err != nil {
				return fmt.Errorf("store agent reading %s: %w", e.ID, err)
			}
			ingested += tag.RowsAffected()
		}
		_, err = tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_seq, ingested_total, skipped_total, agent_lost, agent_depth, last_pull_at, last_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), '')
ON CONFLICT (server_id) DO UPDATE SET
    last_seq = excluded.last_seq,
    ingested_total = server_agent_event_cursors.ingested_total + excluded.ingested_total,
    skipped_total = server_agent_event_cursors.skipped_total + excluded.skipped_total,
    agent_lost = excluded.agent_lost,
    agent_depth = excluded.agent_depth,
    last_pull_at = now(), last_error = '', updated_at = now()`,
			serverID, tenantID, int64(through), ingested, skipped, int64(min(page.Lost, 1<<62)), page.Depth) //nolint:gosec // bounded above and below
		return err
	})
	if err != nil {
		return cursor, err
	}
	return through, nil
}

// serverCameras returns the cameras central knows for the server, with their site: the only ones a
// reading may be attributed to. An agent cannot file a reading under a camera of another server.
func serverCameras(ctx context.Context, tx pgx.Tx, serverID, tenantID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id, site_id FROM cameras WHERE server_id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, serverID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var id, site uuid.UUID
		if err := rows.Scan(&id, &site); err != nil {
			return nil, err
		}
		out[id] = site
	}
	return out, rows.Err()
}

// saveCursorOnly moves the cursor without touching the counters.
func (a *AgentIngestor) saveCursorOnly(ctx context.Context, serverID, tenantID uuid.UUID, seq int64) error {
	return a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_seq) VALUES ($1, $2, $3)
ON CONFLICT (server_id) DO UPDATE SET last_seq = excluded.last_seq, updated_at = now()`, serverID, tenantID, seq)
		return err
	})
}

// touch records a successful pull that returned nothing new.
func (a *AgentIngestor) touch(ctx context.Context, serverID, tenantID uuid.UUID, page provision.LPREventsPage) error {
	return a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, agent_lost, agent_depth, last_pull_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (server_id) DO UPDATE SET agent_lost = excluded.agent_lost, agent_depth = excluded.agent_depth,
    last_pull_at = now(), last_error = '', updated_at = now()`,
			serverID, tenantID, int64(min(page.Lost, 1<<62)), page.Depth) //nolint:gosec // bounded above
		return err
	})
}

// recordError keeps the last failure where an operator can see it. It never hides the original error.
func (a *AgentIngestor) recordError(ctx context.Context, serverID, tenantID uuid.UUID, cause error) {
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_pull_at, last_error) VALUES ($1, $2, now(), $3)
ON CONFLICT (server_id) DO UPDATE SET last_pull_at = now(), last_error = excluded.last_error, updated_at = now()`,
			serverID, tenantID, msg)
		return err
	})
	if err != nil {
		a.Log.WarnContext(ctx, "agent ingest: record error", "server_id", serverID, "error", err)
	}
}
