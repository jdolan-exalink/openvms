package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionCameraHistoryPurged records the purge of a deleted camera's history.
const ActionCameraHistoryPurged = "CAMERA_HISTORY_PURGED"

// purgeBatch is how many due cameras are listed at a time; every due camera is still attempted per run.
var purgeBatch int32 = 50

// PurgeDeletedCameraHistory removes the history OpenVMS keeps for deleted cameras whose recording
// window has passed: events, plate reads, alarms, tracks, watermarked clips, notifications and rule
// firings, and the stored objects they point to. The window is the
// retain_days their tombstone recorded when they were deleted; a camera with an unknown window is never
// purged. Exports and export jobs, face sightings and object snapshots are kept. The camera row and its
// tombstone stay, so the sync still skips the name and the camera can still be restored (without that
// history). The stored objects are deleted first, while the camera's tombstone is locked, and the rows
// only afterwards in the same transaction: a failure or a crash in between leaves the rows (and so the
// keys) in place and the camera is retried on the next run, instead of orphaning objects. A camera whose
// purge fails is logged and skipped for the rest of the run, so it cannot hold back the others; every
// due camera is attempted in each run. It returns how many cameras were purged.
func (s *Service) PurgeDeletedCameraHistory(ctx context.Context, now time.Time) (int, error) {
	purged := 0
	skip := []uuid.UUID{}
	for {
		var due []uuid.UUID
		err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			var err error
			due, err = q.ListDueCameraPurges(ctx, db.ListDueCameraPurgesParams{Now: now, Batch: purgeBatch, Skip: skip})
			return err
		})
		if err != nil {
			return purged, err
		}
		if len(due) == 0 {
			return purged, nil
		}
		for _, id := range due {
			var done bool
			err := retryOnDeadlock(ctx, func() error {
				done = false
				return s.purgeCameraHistoryTx(ctx, id, now, &done)
			})
			if err != nil {
				s.Log.WarnContext(ctx, "inventory: purge history of a deleted camera", "camera_id", id, "error", err)
			}
			if err != nil || !done {
				skip = append(skip, id) // failed, or locked by another replica: not again in this run
				continue
			}
			purged++
		}
		if ctx.Err() != nil {
			return purged, ctx.Err()
		}
	}
}

func (s *Service) purgeCameraHistoryTx(ctx context.Context, id uuid.UUID, now time.Time, done *bool) error {
	return s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		tomb, err := q.LockCameraTombstoneForPurge(ctx, db.LockCameraTombstoneForPurgeParams{CameraID: id, Now: now})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // restored, purged or locked by someone else since it was listed
		}
		if err != nil {
			return err
		}
		keys, err := q.ListCameraBlobKeys(ctx, id)
		if err != nil {
			return err
		}
		// Objects first, under the tombstone's row lock, then the rows that name them. Deleting an
		// object that is already gone succeeds, so a retry after a failure is safe.
		if s.Blobs != nil {
			for _, key := range keys {
				if err := s.Blobs.Delete(ctx, key); err != nil {
					return fmt.Errorf("delete stored object %s: %w", key, err)
				}
			}
		}
		counts := map[string]int64{}
		steps := []struct {
			name string
			run  func() (int64, error)
		}{
			{"clip_watermark_jobs", func() (int64, error) { return q.PurgeCameraClipJobs(ctx, id) }},
			{"alarms", func() (int64, error) { return q.PurgeCameraAlarms(ctx, id) }},
			{"lpr_reads", func() (int64, error) { return q.PurgeCameraLprReads(ctx, id) }},
			{"events", func() (int64, error) { return q.PurgeCameraEvents(ctx, id) }},
			{"object_tracks", func() (int64, error) { return q.PurgeCameraObjectTracks(ctx, id) }},
			{"notifications", func() (int64, error) { return q.PurgeCameraNotifications(ctx, &id) }},
			{"rule_firings", func() (int64, error) { return q.PurgeCameraRuleFirings(ctx, id) }},
		}
		for _, st := range steps {
			n, err := st.run()
			if err != nil {
				return fmt.Errorf("purge camera %s: %w", st.name, err)
			}
			counts[st.name] = n
		}
		if err := q.MarkCameraTombstonePurged(ctx, id); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]any{"remote_name": tomb.RemoteName, "server_id": tomb.ServerID, "retain_days": tomb.RetainDays, "deleted": counts})
		if err != nil {
			return err
		}
		*done = true
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &tomb.TenantID, ActorName: "system", Action: ActionCameraHistoryPurged,
			TargetType: "camera", TargetID: &id, Details: details,
		})
	})
}

// CameraHistoryPurger runs PurgeDeletedCameraHistory on an interval. Several replicas can run it: each
// camera is locked while it is purged.
type CameraHistoryPurger struct {
	Svc      *Service
	Interval time.Duration
	Log      *slog.Logger
}

// DefaultCameraPurgeInterval is how often due histories are looked for.
const DefaultCameraPurgeInterval = time.Hour

// Run purges once and then every Interval until ctx ends.
func (p *CameraHistoryPurger) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultCameraPurgeInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		n, err := p.Svc.PurgeDeletedCameraHistory(ctx, time.Now())
		switch {
		case err != nil && ctx.Err() == nil:
			p.Log.WarnContext(ctx, "inventory: purge deleted camera history", "error", err)
		case n > 0:
			p.Log.InfoContext(ctx, "inventory: purged the history of deleted cameras", "cameras", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
