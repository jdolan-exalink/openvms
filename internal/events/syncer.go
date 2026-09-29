// Package events builds the central index of Frigate review items and plate reads
// (PRD §34-45). The syncer pulls from every Frigate server with cursors kept in the
// database, so after an outage of the VMS or of a Frigate it backfills from where it
// stopped; the service answers searches and serves thumbnails under the caller's
// permissions.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Blobs stores thumbnails; *objectstore.Store implements it.
type Blobs interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
}

var _ Blobs = (*objectstore.Store)(nil)

// Syncer pulls review items and plate reads from every registered Frigate server.
type Syncer struct {
	Store    *store.Store
	Adapters *inventory.Adapters
	Blobs    Blobs
	Log      *slog.Logger
	// Interval between pulls of one server. Frigate is polled rather than subscribed over
	// MQTT so a server needs nothing but its HTTP port; a few seconds of delay is fine for
	// an index (live view does not depend on it).
	Interval time.Duration
	// Backfill is how far back a newly registered server is imported.
	Backfill time.Duration
	// Concurrency bounds how many servers are pulled at the same time.
	Concurrency int
	// OnNew is called for each review item seen for the first time (optional).
	OnNew func(ctx context.Context, e NewEvent)
	// OnAlarm is called after commit with the alarms opened by one review pull (optional).
	OnAlarm func(ctx context.Context, alarms []OpenedAlarm)

	now func() time.Time
}

// NewEvent is published when an event enters the index.
type NewEvent struct {
	ID       uuid.UUID `json:"id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	ServerID uuid.UUID `json:"server_id"`
	CameraID uuid.UUID `json:"camera_id"`
	Severity string    `json:"severity"`
	Labels   []string  `json:"labels"`
	Start    time.Time `json:"start_time"`
}

// OpenedAlarm is published when an alert event opens an alarm.
type OpenedAlarm struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	SiteID    uuid.UUID `json:"site_id"`
	CameraID  uuid.UUID `json:"camera_id"`
	EventID   uuid.UUID `json:"event_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	reviewPage = 500
	objectPage = 500
	// Items are re-read this far behind the cursor to pick up updates (end time, late
	// plates, new labels) of items that were still open at the previous pull.
	reviewOverlap = 2 * time.Minute
	objectOverlap = 10 * time.Minute
	// Thumbnails copied per server and pull; the rest wait for the next pull.
	thumbsPerPull    = 40
	maxThumbAttempts = 5
)

func (s *Syncer) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Run pulls every server each Interval until ctx ends.
func (s *Syncer) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		s.SyncAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SyncAll pulls every active server once.
func (s *Syncer) SyncAll(ctx context.Context) {
	var servers []db.FrigateServer
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		servers, err = q.ListAllActiveServers(ctx)
		return err
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "events: list servers", "error", err)
		return
	}
	sem := make(chan struct{}, max(s.Concurrency, 1))
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if err := s.SyncServer(cctx, srv); err != nil && ctx.Err() == nil {
				s.Log.WarnContext(ctx, "events: sync server", "server_id", srv.ID, "error", err)
			}
		}()
	}
	wg.Wait()
}

type syncState struct {
	reviewCursor *time.Time
	objectCursor *time.Time
}

// SyncServer pulls one server: review items, plate reads, then pending thumbnails.
func (s *Syncer) SyncServer(ctx context.Context, srv db.FrigateServer) error {
	a, err := s.Adapters.Get(ctx, srv)
	if err != nil {
		s.recordError(ctx, srv, err)
		return err
	}
	cams, st, err := s.load(ctx, srv)
	if err != nil {
		return err
	}
	syncErr := s.syncReviews(ctx, srv, a, cams, st)
	if syncErr == nil {
		// Tracked objects are synced for every camera, not only LPR-tagged ones: besides plate
		// extraction (LPR cameras only, unaffected), each object also carries has_snapshot
		// (PRD §44), which applies to every camera.
		syncErr = s.syncObjects(ctx, srv, a, cams, st)
	}
	if syncErr != nil {
		if errors.Is(syncErr, frigate.ErrUnauthorized) {
			s.Adapters.Forget(srv.ID)
		}
		s.recordError(ctx, srv, syncErr)
		return syncErr
	}
	s.copyThumbnails(ctx, srv, a)
	return nil
}

type cameraRef struct {
	ID  uuid.UUID
	LPR bool
}

func (s *Syncer) load(ctx context.Context, srv db.FrigateServer) (map[string]cameraRef, syncState, error) {
	cams := map[string]cameraRef{}
	var st syncState
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := db.New(tx).ListServerCameras(ctx, srv.ID)
		if err != nil {
			return err
		}
		for _, c := range rows {
			cams[c.RemoteName] = cameraRef{ID: c.ID, LPR: c.Lpr}
		}
		err = tx.QueryRow(ctx, `SELECT review_cursor, object_cursor FROM event_sync_state WHERE server_id = $1`, srv.ID).
			Scan(&st.reviewCursor, &st.objectCursor)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return cams, st, err
}

func unix(t time.Time) float64 { return float64(t.UnixMicro()) / 1e6 }

func fromUnix(f float64) time.Time {
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC()
}

// syncReviews reads review items newer than the cursor. Frigate returns them newest
// first, so a full page means there is more: the next page asks for items before the
// oldest one seen.
func (s *Syncer) syncReviews(ctx context.Context, srv db.FrigateServer, a frigate.Adapter, cams map[string]cameraRef, st syncState) error {
	from := s.clock().Add(-s.Backfill)
	if st.reviewCursor != nil {
		from = st.reviewCursor.Add(-reviewOverlap)
	}
	var all []frigate.Review
	before := 0.0
	for page := 0; page < 40; page++ {
		items, err := a.Reviews(ctx, frigate.ReviewQuery{After: unix(from), Before: before, Limit: reviewPage})
		if err != nil {
			return err
		}
		all = append(all, items...)
		if len(items) < reviewPage {
			break
		}
		oldest := items[len(items)-1].StartTime
		if before != 0 && oldest >= before {
			break // Frigate ignored "before"; avoid looping on the same page
		}
		before = oldest
	}
	if len(all) == 0 {
		return s.saveCursor(ctx, srv, "review_cursor", st.reviewCursor)
	}

	// The next pull starts at the newest item, or at the oldest one still open so its end
	// time is picked up. Items open for more than an hour (Frigate restarted mid-review
	// and never closed them) do not hold the cursor back.
	var next time.Time
	var openFrom *time.Time
	stale := s.clock().Add(-time.Hour)
	for _, r := range all {
		t := fromUnix(r.StartTime)
		if t.After(next) {
			next = t
		}
		if r.EndTime == nil && t.After(stale) && (openFrom == nil || t.Before(*openFrom)) {
			openFrom = &t
		}
	}
	if openFrom != nil && openFrom.Before(next) {
		next = *openFrom
	} else if st.reviewCursor != nil && next.Before(*st.reviewCursor) {
		next = *st.reviewCursor
	}

	var created []NewEvent
	var opened []OpenedAlarm
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, r := range all {
			cam, ok := cams[r.Camera]
			if !ok {
				continue // camera not imported (or removed); the review has nowhere to go
			}
			ev, isNew, err := upsertReview(ctx, tx, srv, cam.ID, cam.LPR, r)
			if err != nil {
				return err
			}
			if isNew {
				created = append(created, ev)
			}
			// On every upsert, not only inserts: a detection promoted to alert opens its
			// alarm later. A downgrade leaves an existing alarm untouched.
			if ev.Severity == "alert" {
				alarm, err := openAlarm(ctx, tx, ev)
				if err != nil {
					return err
				}
				if alarm != nil {
					opened = append(opened, *alarm)
				}
			}
		}
		return upsertCursor(ctx, tx, srv, "review_cursor", &next)
	})
	if err != nil {
		return err
	}
	if s.OnNew != nil {
		for _, ev := range created {
			s.OnNew(ctx, ev)
		}
	}
	if s.OnAlarm != nil && len(opened) > 0 {
		s.OnAlarm(ctx, opened)
	}
	return nil
}

// openAlarm opens the event's alarm unless one already exists (unique per event and source, so
// re-reads are idempotent) or the event started before alarms were enabled (alarm_settings:
// there is no backfill). It returns nil when nothing was opened.
func openAlarm(ctx context.Context, tx pgx.Tx, ev NewEvent) (*OpenedAlarm, error) {
	a := OpenedAlarm{TenantID: ev.TenantID, SiteID: ev.SiteID, CameraID: ev.CameraID, EventID: ev.ID}
	err := tx.QueryRow(ctx, `
INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source)
SELECT $1, $2, $3, $4, 'event'
WHERE $5::timestamptz >= (SELECT enabled_at FROM alarm_settings)
ON CONFLICT (event_id, source) DO NOTHING
RETURNING id, status, created_at`,
		a.TenantID, a.SiteID, a.CameraID, a.EventID, ev.Start,
	).Scan(&a.ID, &a.Status, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open alarm for event %s: %w", ev.ID, err)
	}
	return &a, nil
}

func severity(v string) string {
	if v == "alert" {
		return "alert"
	}
	return "detection"
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func upsertReview(ctx context.Context, tx pgx.Tx, srv db.FrigateServer, cameraID uuid.UUID, cameraLPR bool, r frigate.Review) (NewEvent, bool, error) {
	var end *time.Time
	if r.EndTime != nil {
		t := fromUnix(*r.EndTime)
		end = &t
	}
	start := fromUnix(r.StartTime)
	detections := nonNil(r.Data.Detections)
	var id uuid.UUID
	var inserted bool
	// Plates come from lpr_reads already stored for any of the review's detections, and
	// has_snapshot from object_snapshots the same way (see markHasSnapshot/upsertObjectSnapshot):
	// both converge on every upsert regardless of whether the review or its tracked objects were
	// seen first. lpr records the camera's LPR capability at ingestion time and, like
	// has_snapshot, is never reset once true (see internal/events/service.go for why: gating
	// must survive a camera later being switched off LPR).
	err := tx.QueryRow(ctx, `
INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, sub_labels, zones, audio,
                    detection_ids, start_time, end_time, reviewed, plates, has_snapshot, lpr)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
        coalesce((SELECT array_agg(DISTINCT l.plate_normalized) FROM lpr_reads l
                  WHERE l.server_id = $3 AND l.remote_event_id = ANY($11::text[])), '{}'),
        EXISTS (SELECT 1 FROM object_snapshots os WHERE os.server_id = $3 AND os.remote_object_id = ANY($11::text[])),
        $15)
ON CONFLICT (server_id, remote_id) DO UPDATE SET
    severity = excluded.severity,
    labels = excluded.labels,
    sub_labels = excluded.sub_labels,
    zones = excluded.zones,
    audio = excluded.audio,
    detection_ids = excluded.detection_ids,
    end_time = excluded.end_time,
    -- Sticky: a review made in the VMS survives Frigate still reporting "not reviewed".
    reviewed = events.reviewed OR excluded.reviewed,
    plates = (SELECT coalesce(array_agg(DISTINCT p), '{}') FROM unnest(events.plates || excluded.plates) p),
    has_snapshot = events.has_snapshot OR excluded.has_snapshot,
    lpr = events.lpr OR excluded.lpr,
    updated_at = now()
RETURNING id, (xmax = 0)`,
		srv.TenantID, srv.SiteID, srv.ID, cameraID, r.ID, severity(r.Severity),
		nonNil(r.Data.Objects), nonNil(r.Data.SubLabels), nonNil(r.Data.Zones), nonNil(r.Data.Audio),
		detections, start, end, r.HasBeenReviewed, cameraLPR,
	).Scan(&id, &inserted)
	if err != nil {
		return NewEvent{}, false, fmt.Errorf("upsert review %s: %w", r.ID, err)
	}
	return NewEvent{
		ID: id, TenantID: srv.TenantID, SiteID: srv.SiteID, ServerID: srv.ID, CameraID: cameraID,
		Severity: severity(r.Severity), Labels: nonNil(r.Data.Objects), Start: start,
	}, inserted, nil
}

// syncObjects reads tracked objects of every camera (oldest first): those with a plate are
// stored in lpr_reads (LPR cameras only, since only they ever carry one), and every object
// with has_snapshot=true marks its parent review's has_snapshot flag (every camera, PRD §44).
func (s *Syncer) syncObjects(ctx context.Context, srv db.FrigateServer, a frigate.Adapter, cams map[string]cameraRef, st syncState) error {
	from := s.clock().Add(-s.Backfill)
	if st.objectCursor != nil {
		from = st.objectCursor.Add(-objectOverlap)
	}
	after := unix(from)
	var reads []frigate.TrackedObject
	var snapshots []frigate.TrackedObject
	newest := from
	for page := 0; page < 40; page++ {
		// No Cameras filter: every camera's objects are read, not only LPR-tagged ones. Plate
		// extraction below is a client-side filter on o.Plate, not a "plates only" request to
		// Frigate: this pass also needs every object's HasSnapshot, on every camera.
		items, err := a.TrackedObjects(ctx, frigate.ObjectQuery{After: after, Limit: objectPage})
		if err != nil {
			return err
		}
		for _, o := range items {
			if t := fromUnix(o.StartTime); t.After(newest) {
				newest = t
			}
			if o.Plate != "" {
				reads = append(reads, o)
			}
			if o.HasSnapshot {
				snapshots = append(snapshots, o)
			}
		}
		if len(items) < objectPage || items[len(items)-1].StartTime <= after {
			break
		}
		after = items[len(items)-1].StartTime
	}
	return s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, o := range reads {
			cam, ok := cams[o.Camera]
			if !ok {
				continue
			}
			if err := upsertPlate(ctx, tx, srv, cam.ID, o); err != nil {
				return err
			}
		}
		for _, o := range snapshots {
			if _, ok := cams[o.Camera]; !ok {
				continue
			}
			// Persist the fact first (mirrors upsertPlate inserting into lpr_reads before
			// updating events.plates): upsertReview derives has_snapshot from object_snapshots
			// on every future upsert, so this converges even if the review that will end up
			// listing this detection is not indexed yet, or does not list it yet.
			if err := upsertObjectSnapshot(ctx, tx, srv, o.ID); err != nil {
				return err
			}
			// Also flip the review immediately if it is already indexed, instead of waiting for
			// its next upsert; this only ever flips has_snapshot from false to true, never back,
			// mirroring upsertPlate's append-only union of plates above.
			if err := markHasSnapshot(ctx, tx, srv, o.ID); err != nil {
				return err
			}
		}
		// The cursor moves to the newest object seen; the next pull re-reads objectOverlap
		// before it for plates recognized after an object started.
		cursor := newest
		if st.objectCursor != nil && cursor.Before(*st.objectCursor) {
			cursor = *st.objectCursor
		}
		if cursor.After(s.clock()) {
			cursor = s.clock()
		}
		return upsertCursor(ctx, tx, srv, "object_cursor", &cursor)
	})
}

// markHasSnapshot sets has_snapshot on the review that contains remoteObjectID, if it is
// already indexed. See PRD §44 ("Has snapshot" filter) and syncObjects above. This is only the
// immediate best-effort path: upsertObjectSnapshot (called just before this, in syncObjects) is
// what makes the fact converge even when the review is not indexed yet, or does not list this
// detection yet.
func markHasSnapshot(ctx context.Context, tx pgx.Tx, srv db.FrigateServer, remoteObjectID string) error {
	_, err := tx.Exec(ctx, `
UPDATE events SET has_snapshot = true, updated_at = now()
WHERE server_id = $1 AND $2 = ANY(detection_ids) AND NOT has_snapshot`, srv.ID, remoteObjectID)
	return err
}

// upsertObjectSnapshot persists that a Frigate tracked object reported has_snapshot=true, the
// same role upsertPlate's insert into lpr_reads plays for plates. upsertReview derives
// events.has_snapshot from this table by detection_ids on every review upsert, so the fact
// survives regardless of arrival order between a review and its tracked objects.
func upsertObjectSnapshot(ctx context.Context, tx pgx.Tx, srv db.FrigateServer, remoteObjectID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO object_snapshots (server_id, tenant_id, remote_object_id)
VALUES ($1, $2, $3)
ON CONFLICT (server_id, remote_object_id) DO NOTHING`, srv.ID, srv.TenantID, remoteObjectID)
	return err
}

// NormalizePlate keeps letters and digits in upper case: "ab 123-cd" → "AB123CD".
func NormalizePlate(p string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(p) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func upsertPlate(ctx context.Context, tx pgx.Tx, srv db.FrigateServer, cameraID uuid.UUID, o frigate.TrackedObject) error {
	norm := NormalizePlate(o.Plate)
	if norm == "" {
		return nil
	}
	var score *float32
	if o.PlateScore != nil {
		v := float32(*o.PlateScore)
		score = &v
	}
	_, err := tx.Exec(ctx, `
INSERT INTO lpr_reads (tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, score, label, sub_label, zones, seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (server_id, remote_event_id) DO UPDATE SET
    plate = excluded.plate,
    plate_normalized = excluded.plate_normalized,
    score = excluded.score,
    sub_label = excluded.sub_label,
    zones = excluded.zones,
    updated_at = now()`,
		srv.TenantID, srv.SiteID, srv.ID, cameraID, o.ID, o.Plate, norm, score, o.Label, o.SubLabel, nonNil(o.Zones), fromUnix(o.StartTime))
	if err != nil {
		return fmt.Errorf("upsert plate %s: %w", o.ID, err)
	}
	// Attach the plate to the review item that contains this object, if already indexed.
	_, err = tx.Exec(ctx, `
UPDATE events SET plates = (SELECT array_agg(DISTINCT p) FROM unnest(plates || ARRAY[$3::text]) p), updated_at = now()
WHERE server_id = $1 AND $2 = ANY(detection_ids) AND NOT ($3 = ANY(plates))`, srv.ID, o.ID, norm)
	return err
}

func upsertCursor(ctx context.Context, tx pgx.Tx, srv db.FrigateServer, column string, v *time.Time) error {
	// column is one of two constants, never user input.
	_, err := tx.Exec(ctx, `
INSERT INTO event_sync_state (server_id, tenant_id, `+column+`, last_success_at, last_error)
VALUES ($1, $2, $3, now(), '')
ON CONFLICT (server_id) DO UPDATE SET `+column+` = excluded.`+column+`, last_success_at = now(), last_error = '', updated_at = now()`,
		srv.ID, srv.TenantID, v)
	return err
}

func (s *Syncer) saveCursor(ctx context.Context, srv db.FrigateServer, column string, v *time.Time) error {
	return s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return upsertCursor(ctx, tx, srv, column, v)
	})
}

func (s *Syncer) recordError(ctx context.Context, srv db.FrigateServer, cause error) {
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO event_sync_state (server_id, tenant_id, last_error) VALUES ($1, $2, $3)
ON CONFLICT (server_id) DO UPDATE SET last_error = excluded.last_error, updated_at = now()`,
			srv.ID, srv.TenantID, truncate(cause.Error(), 500))
		return err
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "events: record sync error", "server_id", srv.ID, "error", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ThumbKey is where the thumbnail of an event lives in object storage (PRD §70 prefix).
func ThumbKey(tenantID, serverID, eventID uuid.UUID) string {
	return fmt.Sprintf("tenant/%s/thumbs/%s/%s", tenantID, serverID, eventID)
}

// copyThumbnails copies thumbnails of finished events so they survive Frigate's retention
// and outages.
func (s *Syncer) copyThumbnails(ctx context.Context, srv db.FrigateServer, a frigate.Adapter) {
	type pending struct {
		id       uuid.UUID
		remoteID string
		camera   string
		start    time.Time
	}
	var todo []pending
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT e.id, e.remote_id, c.remote_name, e.start_time FROM events e JOIN cameras c ON c.id = e.camera_id
WHERE e.server_id = $1 AND e.thumb_key = '' AND e.end_time IS NOT NULL AND e.thumb_attempts < $2
ORDER BY e.start_time DESC LIMIT $3`, srv.ID, maxThumbAttempts, thumbsPerPull)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.remoteID, &p.camera, &p.start); err != nil {
				return err
			}
			todo = append(todo, p)
		}
		return rows.Err()
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "events: list pending thumbnails", "server_id", srv.ID, "error", err)
		return
	}
	for _, p := range todo {
		review := frigate.Review{ID: p.remoteID, Camera: p.camera, ThumbPath: fmt.Sprintf("/media/frigate/clips/review/thumb-%s-%s.webp", p.camera, p.remoteID)}
		body, ct, err := a.ReviewThumbnail(ctx, review)
		key := ""
		if err == nil {
			key = ThumbKey(srv.TenantID, srv.ID, p.id)
			if err = s.Blobs.Put(ctx, key, body, ct); err != nil {
				key = ""
			}
		}
		if err != nil && !errors.Is(err, frigate.ErrNotFound) {
			s.Log.DebugContext(ctx, "events: copy thumbnail", "event_id", p.id, "error", err)
		}
		uerr := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE events SET thumb_key = $2, thumb_attempts = thumb_attempts + 1 WHERE id = $1`, p.id, key)
			return err
		})
		if uerr != nil {
			s.Log.ErrorContext(ctx, "events: store thumbnail key", "event_id", p.id, "error", uerr)
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}
