package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// staleReviewsPerPull bounds how many open events are re-checked per server and pull.
const staleReviewsPerPull = 50

// A review open longer than StaleOpenWindow no longer holds the review cursor back (see
// syncReviews), so it is never re-read by the regular pull. closeStaleReviews resolves those rows
// against Frigate instead: an end_time that was set late is picked up, and a review Frigate lost
// (restart mid-review, retention purge) is closed rather than left running forever.

type staleVerdict int

const (
	// staleLeave: Frigate still reports it open, or the answer cannot prove it is gone.
	staleLeave staleVerdict = iota
	// staleSetFromFrigate: Frigate has it closed; take its data.
	staleSetFromFrigate
	// staleCloseAtLastActivity: Frigate no longer has it.
	staleCloseAtLastActivity
)

// staleReviewVerdict decides what to do with an open event given Frigate's answer to
// GET /api/review/{id}: the review, or the error. Only a 404 proves Frigate lost it; any other
// failure proves nothing, so the row is left for the next pull.
func staleReviewVerdict(found *frigate.Review, err error) staleVerdict {
	switch {
	case errors.Is(err, frigate.ErrNotFound):
		return staleCloseAtLastActivity
	case err != nil || found == nil:
		return staleLeave
	case found.EndTime != nil:
		return staleSetFromFrigate
	default:
		return staleLeave
	}
}

// lastKnownActivity is when an event gone from Frigate was last seen alive: the latest sample of
// its tracked objects (object_tracks end_time or last path point), never before its own start.
func lastKnownActivity(start time.Time, tracks *time.Time) time.Time {
	if tracks != nil && tracks.After(start) {
		return *tracks
	}
	return start
}

type staleRow struct {
	remoteID string
	cameraID uuid.UUID
	start    time.Time
	tracks   *time.Time
}

// closeStaleReviews re-checks open events older than StaleOpenWindow against Frigate. Errors are
// logged and skipped: a failing Frigate call must not fail the rest of the sync.
func (s *Syncer) closeStaleReviews(ctx context.Context, srv db.FrigateServer, a frigate.Adapter, cams map[string]cameraRef) {
	cutoff := s.clock().Add(-StaleOpenWindow)
	var rows []staleRow
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		r, err := tx.Query(ctx, `
SELECT e.remote_id, e.camera_id, e.start_time,
       (SELECT max(GREATEST(ot.end_time, to_timestamp((SELECT max((p->>'t')::float8) FROM jsonb_array_elements(ot.path) p))))
        FROM object_tracks ot WHERE ot.server_id = e.server_id AND ot.remote_object_id = ANY(e.detection_ids))
FROM events e
WHERE e.server_id = $1 AND e.end_time IS NULL AND e.start_time < $2
ORDER BY e.start_time ASC
LIMIT $3`, srv.ID, cutoff, staleReviewsPerPull)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var row staleRow
			if err := r.Scan(&row.remoteID, &row.cameraID, &row.start, &row.tracks); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return r.Err()
	})
	if err != nil {
		s.Log.WarnContext(ctx, "events: list stale open reviews", "server_id", srv.ID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	byID := map[uuid.UUID]string{}
	for name, c := range cams {
		byID[c.ID] = name
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		name, ok := byID[row.cameraID]
		if !ok {
			continue // camera removed; nothing to resolve against
		}
		s.resolveStaleReview(ctx, srv, a, cams[name], row)
	}
}

// resolveStaleReview asks Frigate for one review (GET /api/review/{id}) and settles it. Errors
// are logged and skipped so one bad row never blocks the others.
func (s *Syncer) resolveStaleReview(ctx context.Context, srv db.FrigateServer, a frigate.Adapter, cam cameraRef, row staleRow) {
	rev, err := a.Review(ctx, row.remoteID)
	var found *frigate.Review
	if err == nil {
		found = &rev
	}
	verdict := staleReviewVerdict(found, err)
	if err != nil && verdict == staleLeave {
		s.Log.WarnContext(ctx, "events: re-read stale review", "server_id", srv.ID, "review", row.remoteID, "error", err)
		return
	}
	err = s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		switch verdict {
		case staleSetFromFrigate:
			_, _, err := upsertReview(ctx, tx, srv, cam.ID, cam.LPR, *found)
			return err
		case staleCloseAtLastActivity:
			end := lastKnownActivity(row.start, row.tracks)
			if _, err := tx.Exec(ctx, `UPDATE events SET end_time = $3, updated_at = now()
WHERE server_id = $1 AND remote_id = $2 AND end_time IS NULL`, srv.ID, row.remoteID, end); err != nil {
				return fmt.Errorf("close stale review %s: %w", row.remoteID, err)
			}
		}
		return nil
	})
	if err != nil {
		s.Log.WarnContext(ctx, "events: close stale review", "server_id", srv.ID, "review", row.remoteID, "error", err)
	}
}
