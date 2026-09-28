package events

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Service answers event and plate searches. Every query is limited in SQL to the cameras
// the actor may see (PRD §31), and runs under the actor's tenant scope (RLS).
type Service struct {
	Store    *store.Store
	Blobs    Blobs
	Adapters *inventory.Adapters
	Log      *slog.Logger
}

// ErrInvalidCursor is returned for a pagination cursor that was not produced by the API.
var ErrInvalidCursor = errors.New("invalid cursor")

type Event struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	SiteID     uuid.UUID
	SiteName   string
	ServerID   uuid.UUID
	ServerName string
	CameraID   uuid.UUID
	CameraName string
	// CameraLPR is the camera's LPR capability (cameras.lpr), used only to decide whether
	// sub_labels needs LPR-gated redaction (see ListEvents/getEvent); it is not exposed via the API.
	CameraLPR    bool
	RemoteID     string
	Severity     string
	Labels       []string
	SubLabels    []string
	Zones        []string
	Plates       []string
	StartTime    time.Time
	EndTime      *time.Time
	Reviewed     bool
	HasThumbnail bool
}

type Filter struct {
	SiteIDs   []uuid.UUID
	ServerIDs []uuid.UUID
	CameraIDs []uuid.UUID
	// CameraGroupIDs restricts to cameras belonging to any of these groups, intersected with
	// (never widening) the cameras the actor may otherwise see.
	CameraGroupIDs []uuid.UUID
	Labels         []string
	Zones          []string
	SubLabels      []string
	Severity       string
	// Plate matches events with a plate containing this text (normalized).
	Plate    string
	From     *time.Time
	To       *time.Time
	Reviewed *bool
	Cursor   string
	Limit    int
}

type Page[T any] struct {
	Items []T
	// Next is the cursor for the following page, empty on the last one.
	Next string
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixMicro(), 10) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	micros, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return time.UnixMicro(micros).UTC(), id, nil
}

func limit(n int) int {
	switch {
	case n <= 0:
		return 50
	case n > 500:
		return 500
	}
	return n
}

// sqlArgs builds a WHERE clause with positional parameters.
type sqlArgs struct {
	where []string
	args  []any
}

func (b *sqlArgs) add(cond string, v any) {
	b.args = append(b.args, v)
	b.where = append(b.where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(b.args))))
}

func (b *sqlArgs) sql() string { return strings.Join(b.where, " AND ") }

func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(tx pgx.Tx, c *access.Checker) error) error {
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		c, err := access.Load(ctx, db.New(tx), actor)
		if err != nil {
			return err
		}
		return fn(tx, c)
	})
}

func idSet(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

const eventColumns = `e.id, e.tenant_id, e.site_id, s.name, e.server_id, fs.name, e.camera_id, c.display_name, c.lpr, e.remote_id,
e.severity, e.labels, e.sub_labels, e.zones, e.plates, e.start_time, e.end_time, e.reviewed, e.thumb_key <> ''`

const eventJoins = `FROM events e
JOIN cameras c ON c.id = e.camera_id
JOIN sites s ON s.id = e.site_id
JOIN frigate_servers fs ON fs.id = e.server_id`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.TenantID, &e.SiteID, &e.SiteName, &e.ServerID, &e.ServerName, &e.CameraID, &e.CameraName, &e.CameraLPR, &e.RemoteID,
		&e.Severity, &e.Labels, &e.SubLabels, &e.Zones, &e.Plates, &e.StartTime, &e.EndTime, &e.Reviewed, &e.HasThumbnail)
	return e, err
}

// ListEvents searches the index newest first. Plates and sub_labels (which carry recognized
// plate text on LPR cameras) are only returned on cameras where the actor holds lpr.view, and
// only searchable on cameras where the actor holds lpr.search.
func (s *Service) ListEvents(ctx context.Context, actor authz.Actor, f Filter) (Page[Event], error) {
	var out Page[Event]
	n := limit(f.Limit)
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		perm := authz.EventsView
		if f.Plate != "" || len(f.Labels) > 0 || len(f.Zones) > 0 || len(f.SubLabels) > 0 || f.From != nil {
			perm = authz.EventsSearch
		}
		cams, err := c.CameraIDs(ctx, perm)
		if err != nil {
			return err
		}
		lprView, err := c.CameraIDs(ctx, authz.LPRView)
		if err != nil {
			return err
		}
		var b sqlArgs
		b.add("e.camera_id = ANY(?)", cams)
		if f.Plate != "" {
			plateCams, err := c.CameraIDs(ctx, authz.LPRSearch)
			if err != nil {
				return err
			}
			b.add("e.camera_id = ANY(?)", plateCams)
			b.add("EXISTS (SELECT 1 FROM unnest(e.plates) p WHERE p LIKE '%' || ? || '%')", NormalizePlate(f.Plate))
		}
		if len(f.SiteIDs) > 0 {
			b.add("e.site_id = ANY(?)", f.SiteIDs)
		}
		if len(f.ServerIDs) > 0 {
			b.add("e.server_id = ANY(?)", f.ServerIDs)
		}
		if len(f.CameraIDs) > 0 {
			b.add("e.camera_id = ANY(?)", f.CameraIDs)
		}
		if len(f.CameraGroupIDs) > 0 {
			// Membership rows are tenant-scoped (RLS), so a cross-tenant group id matches no
			// row here; combined with the e.camera_id = ANY(cams) clause above, a group that
			// contains a camera the actor cannot otherwise see never leaks it.
			b.add("EXISTS (SELECT 1 FROM camera_group_members m WHERE m.camera_id = e.camera_id AND m.group_id = ANY(?))", f.CameraGroupIDs)
		}
		if len(f.Labels) > 0 {
			b.add("e.labels && ?", f.Labels)
		}
		if len(f.Zones) > 0 {
			b.add("e.zones && ?", f.Zones)
		}
		if len(f.SubLabels) > 0 {
			// sub_label carries recognized plate text only on LPR-capable cameras
			// (frigatemock.enrich); filtering by it there is gated the same as the Plate filter
			// above, requiring lpr.search (events.search alone is not enough). On a non-LPR
			// camera sub_label is an ordinary event field (e.g. a Frigate face-recognition name)
			// and follows only the normal events.search scoping already applied via `cams` above
			// (user decision 2026-09-28). c.lpr comes from the cameras join in eventJoins and is
			// a NOT NULL column, so there is no "unknown" camera to fail open on here.
			lprSearchCams, err := c.CameraIDs(ctx, authz.LPRSearch)
			if err != nil {
				return err
			}
			b.add("(NOT c.lpr OR e.camera_id = ANY(?))", lprSearchCams)
			b.add("e.sub_labels && ?", f.SubLabels)
		}
		if f.Severity != "" {
			b.add("e.severity = ?", f.Severity)
		}
		if f.From != nil {
			b.add("e.start_time >= ?", *f.From)
		}
		if f.To != nil {
			b.add("e.start_time < ?", *f.To)
		}
		if f.Reviewed != nil {
			b.add("e.reviewed = ?", *f.Reviewed)
		}
		if f.Cursor != "" {
			t, id, err := decodeCursor(f.Cursor)
			if err != nil {
				return err
			}
			b.args = append(b.args, t, id)
			b.where = append(b.where, fmt.Sprintf("(e.start_time, e.id) < ($%d, $%d)", len(b.args)-1, len(b.args)))
		}
		b.args = append(b.args, n+1)
		q := "SELECT " + eventColumns + " " + eventJoins + " WHERE " + b.sql() +
			fmt.Sprintf(" ORDER BY e.start_time DESC, e.id DESC LIMIT $%d", len(b.args))
		rows, err := tx.Query(ctx, q, b.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		canPlates := idSet(lprView)
		for rows.Next() {
			e, err := scanEvent(rows)
			if err != nil {
				return err
			}
			if !canPlates[e.CameraID] {
				e.Plates = []string{}
				if e.CameraLPR {
					// sub_labels carry plate text only on LPR-capable cameras, so they are
					// redacted there under the same lpr.view gate as plates. On a non-LPR camera
					// sub_label is not plate data (e.g. a face name), so it stays visible to any
					// actor with events.view (user decision 2026-09-28).
					e.SubLabels = []string{}
				}
			}
			out.Items = append(out.Items, e)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > n {
		last := out.Items[n-1]
		out.Items = out.Items[:n]
		out.Next = encodeCursor(last.StartTime, last.ID)
	}
	if out.Items == nil {
		out.Items = []Event{}
	}
	return out, nil
}

// getEvent loads one event and checks p on its camera. Events of other tenants are
// invisible (RLS) and answer ErrNotFound.
func getEvent(ctx context.Context, tx pgx.Tx, c *access.Checker, p authz.Permission, id uuid.UUID) (Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, "SELECT "+eventColumns+" "+eventJoins+" WHERE e.id = $1", id))
	if err != nil {
		return e, store.Classify(err)
	}
	cam, err := db.New(tx).GetCamera(ctx, e.CameraID)
	if err != nil {
		return e, store.Classify(err)
	}
	if err := c.Require(p, access.Camera(cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, cam.GroupIds)); err != nil {
		return e, err
	}
	if !c.Can(authz.LPRView, access.Camera(cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, cam.GroupIds)) {
		e.Plates = []string{}
		if cam.Lpr {
			// See ListEvents: sub_labels are plate data (and stay redacted) only on LPR-capable
			// cameras; on other cameras they follow normal events.view (user decision 2026-09-28).
			e.SubLabels = []string{}
		}
	}
	return e, nil
}

func (s *Service) GetEvent(ctx context.Context, actor authz.Actor, id uuid.UUID) (Event, error) {
	var out Event
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		var err error
		out, err = getEvent(ctx, tx, c, authz.EventsView, id)
		return err
	})
	return out, err
}

// Thumbnail returns the stored copy, or asks the origin Frigate when it was not copied yet.
func (s *Service) Thumbnail(ctx context.Context, actor authz.Actor, id uuid.UUID) ([]byte, string, error) {
	var e Event
	var key string
	var srv db.FrigateServer
	var cameraRemote string
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		var err error
		if e, err = getEvent(ctx, tx, c, authz.EventsView, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT thumb_key FROM events WHERE id = $1`, id).Scan(&key); err != nil {
			return err
		}
		cam, err := db.New(tx).GetCamera(ctx, e.CameraID)
		if err != nil {
			return err
		}
		cameraRemote = cam.RemoteName
		srv, err = serverRow(ctx, tx, e.ServerID)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if key != "" {
		b, ct, err := s.Blobs.Get(ctx, key)
		if err == nil {
			return b, ct, nil
		}
		if !errors.Is(err, objectstore.ErrNotFound) {
			return nil, "", err
		}
	}
	a, err := s.Adapters.Get(ctx, srv)
	if err != nil {
		return nil, "", &inventory.FrigateError{Err: err}
	}
	b, ct, err := a.ReviewThumbnail(ctx, frigate.Review{ID: e.RemoteID, Camera: cameraRemote,
		ThumbPath: fmt.Sprintf("/media/frigate/clips/review/thumb-%s-%s.webp", cameraRemote, e.RemoteID)})
	if errors.Is(err, frigate.ErrNotFound) {
		return nil, "", store.ErrNotFound
	}
	if err != nil {
		return nil, "", &inventory.FrigateError{Err: err}
	}
	return b, ct, nil
}

// serverRow reads the full server row (with its sealed credentials) for internal use.
func serverRow(ctx context.Context, tx pgx.Tx, id uuid.UUID) (db.FrigateServer, error) {
	srv, err := db.New(tx).GetServerRow(ctx, id)
	return srv, store.Classify(err)
}

// SetReviewed marks an event reviewed in the VMS index (events.review). Frigate's own
// review state is not changed: each operator team keeps its own.
func (s *Service) SetReviewed(ctx context.Context, actor authz.Actor, id uuid.UUID, reviewed bool) (Event, error) {
	var out Event
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		e, err := getEvent(ctx, tx, c, authz.EventsReview, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE events SET reviewed = $2, updated_at = now() WHERE id = $1`, id, reviewed); err != nil {
			return err
		}
		e.Reviewed = reviewed
		out = e
		return nil
	})
	return out, err
}

type PlateRead struct {
	ID         uuid.UUID
	SiteID     uuid.UUID
	SiteName   string
	ServerID   uuid.UUID
	ServerName string
	CameraID   uuid.UUID
	CameraName string
	Plate      string
	Normalized string
	Score      *float32
	Label      string
	Zones      []string
	SeenAt     time.Time
	// EventID is the indexed review item that contains this read, when known.
	EventID *uuid.UUID
}

type PlateFilter struct {
	// Plate matches plates containing this text; Exact requires the whole plate.
	Plate     string
	Exact     bool
	SiteIDs   []uuid.UUID
	CameraIDs []uuid.UUID
	// CameraGroupIDs restricts to cameras belonging to any of these groups, intersected with
	// (never widening) the cameras the actor may otherwise see.
	CameraGroupIDs []uuid.UUID
	From           *time.Time
	To             *time.Time
	Cursor         string
	Limit          int
}

// ListPlates searches plate reads across every Frigate the actor can see (PRD §42).
func (s *Service) ListPlates(ctx context.Context, actor authz.Actor, f PlateFilter) (Page[PlateRead], error) {
	var out Page[PlateRead]
	n := limit(f.Limit)
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		perm := authz.LPRView
		if f.Plate != "" {
			perm = authz.LPRSearch
		}
		cams, err := c.CameraIDs(ctx, perm)
		if err != nil {
			return err
		}
		var b sqlArgs
		b.add("l.camera_id = ANY(?)", cams)
		if p := NormalizePlate(f.Plate); p != "" {
			if f.Exact {
				b.add("l.plate_normalized = ?", p)
			} else {
				b.add("l.plate_normalized LIKE '%' || ? || '%'", p)
			}
		}
		if len(f.SiteIDs) > 0 {
			b.add("l.site_id = ANY(?)", f.SiteIDs)
		}
		if len(f.CameraIDs) > 0 {
			b.add("l.camera_id = ANY(?)", f.CameraIDs)
		}
		if len(f.CameraGroupIDs) > 0 {
			// Same reasoning as ListEvents: membership rows are tenant-scoped (RLS), and this
			// clause only narrows the already-permitted l.camera_id = ANY(cams) set above.
			b.add("EXISTS (SELECT 1 FROM camera_group_members m WHERE m.camera_id = l.camera_id AND m.group_id = ANY(?))", f.CameraGroupIDs)
		}
		if f.From != nil {
			b.add("l.seen_at >= ?", *f.From)
		}
		if f.To != nil {
			b.add("l.seen_at < ?", *f.To)
		}
		if f.Cursor != "" {
			t, id, err := decodeCursor(f.Cursor)
			if err != nil {
				return err
			}
			b.args = append(b.args, t, id)
			b.where = append(b.where, fmt.Sprintf("(l.seen_at, l.id) < ($%d, $%d)", len(b.args)-1, len(b.args)))
		}
		b.args = append(b.args, n+1)
		q := `SELECT l.id, l.site_id, s.name, l.server_id, fs.name, l.camera_id, c.display_name, l.plate, l.plate_normalized,
       l.score, l.label, l.zones, l.seen_at,
       (SELECT e.id FROM events e WHERE e.server_id = l.server_id AND l.remote_event_id = ANY(e.detection_ids) LIMIT 1)
FROM lpr_reads l
JOIN cameras c ON c.id = l.camera_id
JOIN sites s ON s.id = l.site_id
JOIN frigate_servers fs ON fs.id = l.server_id
WHERE ` + b.sql() + fmt.Sprintf(" ORDER BY l.seen_at DESC, l.id DESC LIMIT $%d", len(b.args))
		rows, err := tx.Query(ctx, q, b.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r PlateRead
			if err := rows.Scan(&r.ID, &r.SiteID, &r.SiteName, &r.ServerID, &r.ServerName, &r.CameraID, &r.CameraName,
				&r.Plate, &r.Normalized, &r.Score, &r.Label, &r.Zones, &r.SeenAt, &r.EventID); err != nil {
				return err
			}
			out.Items = append(out.Items, r)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > n {
		last := out.Items[n-1]
		out.Items = out.Items[:n]
		out.Next = encodeCursor(last.SeenAt, last.ID)
	}
	if out.Items == nil {
		out.Items = []PlateRead{}
	}
	return out, nil
}

// SyncStatus is the ingestion state of one server, for the health screen.
type SyncStatus struct {
	ServerID      uuid.UUID
	ReviewCursor  *time.Time
	ObjectCursor  *time.Time
	LastSuccessAt *time.Time
	LastError     string
	EventCount    int64
}

// ListSyncStatus reports ingestion state for the servers the actor can view.
func (s *Service) ListSyncStatus(ctx context.Context, actor authz.Actor) ([]SyncStatus, error) {
	var out []SyncStatus
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		ids, err := c.ServerIDs(ctx, authz.ServersView)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
SELECT fs.id, st.review_cursor, st.object_cursor, st.last_success_at, coalesce(st.last_error, ''),
       (SELECT count(*) FROM events e WHERE e.server_id = fs.id)
FROM frigate_servers fs LEFT JOIN event_sync_state st ON st.server_id = fs.id
WHERE fs.deleted_at IS NULL AND fs.id = ANY($1)
ORDER BY fs.name`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var st SyncStatus
			if err := rows.Scan(&st.ServerID, &st.ReviewCursor, &st.ObjectCursor, &st.LastSuccessAt, &st.LastError, &st.EventCount); err != nil {
				return err
			}
			out = append(out, st)
		}
		return rows.Err()
	})
	if out == nil {
		out = []SyncStatus{}
	}
	return out, err
}
