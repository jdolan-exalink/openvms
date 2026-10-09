package events

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"github.com/jdolan-exalink/openvms/internal/vehicle"
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
	// CameraLPR is the camera's CURRENT LPR capability (cameras.lpr). LPR is this event's own
	// ingestion-time LPR flag (events.lpr). Both are used only to decide whether sub_labels needs
	// LPR-gated redaction (see ListEvents/getEvent, gated on CameraLPR || LPR): keying on the
	// current camera flag alone let a camera switched off LPR after ingestion unlock its
	// historical plate-carrying sub_labels (SECURITY review finding on commit 7658e50). Neither
	// is exposed via the API.
	CameraLPR    bool
	LPR          bool
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
	// HasSnapshot mirrors Frigate's own Event.has_snapshot for any detection of this review.
	HasSnapshot bool
	// HasPreview is true once the event's preview clip has been copied to central storage
	// (preview_key populated); an OpenVMS-internal signal, not a Frigate probe (PRD §20/§44).
	HasPreview bool
	// Vehicle is set once enrichment has stored type and color. Nil until then.
	Vehicle *VehicleAttr
	// Person is set once clothing colors are stored. Nil until then.
	Person *PersonAttr
	// VehicleJob and PersonJob are pending, processing, completed, or failed
	// while a classification job exists. Empty when the event was never queued.
	VehicleJob string
	PersonJob  string
	// Tracks are the Frigate tracked objects behind this event (via detection_ids). Never nil
	// after a read; empty when none were recorded.
	Tracks []Track
}

// Track is one tracked object's real observed trajectory. Box is Frigate's latest bounding box
// only; it is not time-indexed. Path points are normalized bottom-center positions with unix
// seconds.
type Track struct {
	ObjectID  string       `json:"object_id"`
	Label     string       `json:"label"`
	Zones     []string     `json:"zones"`
	Box       []float64    `json:"box"`
	Path      []TrackPoint `json:"path"`
	StartTime time.Time    `json:"start_time"`
	EndTime   *time.Time   `json:"end_time"`
}

// TrackPoint is one observed position of a tracked object.
type TrackPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	T float64 `json:"t"`
}

// TracksSubquery aggregates the tracks of the event aliased e as one jsonb array in the shape of
// Track. The export manifest reuses it so frozen manifests carry the same JSON as the API.
const TracksSubquery = `(SELECT COALESCE(jsonb_agg(jsonb_build_object('object_id', ot.remote_object_id, 'label', ot.label,
	'zones', ot.zones, 'box', ot.box, 'path', ot.path, 'start_time', ot.start_time, 'end_time', ot.end_time)
	ORDER BY ot.start_time), '[]'::jsonb)
	FROM object_tracks ot WHERE ot.server_id = e.server_id AND ot.remote_object_id = ANY(e.detection_ids))`

func parseTracks(raw []byte) ([]Track, error) {
	out := []Track{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Track{}
	}
	return out, nil
}

// PersonAttr is the upper and lower clothing color of one person.
type PersonAttr struct {
	UpperColor      string
	UpperConfidence float32
	LowerColor      string
	LowerConfidence float32
	ColorQuality    string
}

// VehicleAttr is the persisted vehicle enrichment for one event.
type VehicleAttr struct {
	Type            string
	TypeConfidence  float32
	Color           string
	ColorConfidence float32
	ColorQuality    string
	// TrailerColor is set for a truck with a trailer. Empty otherwise.
	TrailerColor           string
	TrailerColorConfidence float32
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
	VehicleTypes   []string
	VehicleColors  []string
	Severity       string
	// Plate matches events with a plate containing this text (normalized).
	Plate string
	From  *time.Time
	To    *time.Time
	// Overlap makes From/To select events overlapping [From, To) instead of events that start
	// inside it. Open events count by the StaleOpenWindow rule.
	Overlap     bool
	Reviewed    *bool
	HasSnapshot *bool
	HasPreview  *bool
	Cursor      string
	Limit       int
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

// StaleOpenWindow is how long an event without end_time is still believed to be running. The
// syncer stops re-reading reviews open longer than this (see syncReviews and closeStaleReviews), so an older open row is stale and must not match every
// later time window. The export manifest and the overlap filter of ListEvents share it.
const StaleOpenWindow = time.Hour

// AliveAtOrAfter is the SQL condition (table alias e) for "the event was running at or after
// fromExpr", a timestamptz expression: it ended then or later, or it is still open and started
// within StaleOpenWindow before it.
func AliveAtOrAfter(fromExpr string) string {
	return fmt.Sprintf("(e.end_time >= %[1]s OR (e.end_time IS NULL AND e.start_time >= %[1]s::timestamptz - interval '%d seconds'))",
		fromExpr, int(StaleOpenWindow.Seconds()))
}

// addTimeFilter applies From/To: events starting inside [From, To), or with Overlap the events
// overlapping it. The keyset cursor (start_time, id) is unaffected: both modes only filter rows.
func addTimeFilter(b *sqlArgs, f Filter) {
	if f.Overlap {
		if f.From != nil {
			b.add(AliveAtOrAfter("?"), *f.From)
		}
		if f.To != nil {
			b.add("e.start_time < ?", *f.To)
		}
		return
	}
	if f.From != nil {
		b.add("e.start_time >= ?", *f.From)
	}
	if f.To != nil {
		b.add("e.start_time < ?", *f.To)
	}
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
e.severity, e.labels, e.sub_labels, e.zones, e.plates, e.start_time, e.end_time, e.reviewed, e.thumb_key <> '',
e.has_snapshot, e.preview_key <> '', e.lpr,
va.vehicle_type, va.vehicle_type_confidence, va.vehicle_color, va.vehicle_color_confidence, va.color_quality,
va.trailer_color, va.trailer_color_confidence,
pa.upper_color, pa.upper_color_confidence, pa.lower_color, pa.lower_color_confidence, pa.color_quality,
vj.status, pj.status, ` + TracksSubquery

const eventJoins = `FROM events e
JOIN cameras c ON c.id = e.camera_id
JOIN sites s ON s.id = e.site_id
JOIN frigate_servers fs ON fs.id = e.server_id
LEFT JOIN vehicle_attributes va ON va.event_id = e.id
LEFT JOIN person_attributes pa ON pa.event_id = e.id
LEFT JOIN vehicle_attribute_jobs vj ON vj.event_id = e.id
LEFT JOIN person_attribute_jobs pj ON pj.event_id = e.id`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	var vType, vColor, vQuality, trailer *string
	var vTypeConf, vColorConf, trailerConf *float32
	var upper, lower, pQuality *string
	var upperConf, lowerConf *float32
	var vehicleJob, personJob *string
	var tracksRaw []byte
	err := row.Scan(&e.ID, &e.TenantID, &e.SiteID, &e.SiteName, &e.ServerID, &e.ServerName, &e.CameraID, &e.CameraName, &e.CameraLPR, &e.RemoteID,
		&e.Severity, &e.Labels, &e.SubLabels, &e.Zones, &e.Plates, &e.StartTime, &e.EndTime, &e.Reviewed, &e.HasThumbnail,
		&e.HasSnapshot, &e.HasPreview, &e.LPR,
		&vType, &vTypeConf, &vColor, &vColorConf, &vQuality, &trailer, &trailerConf,
		&upper, &upperConf, &lower, &lowerConf, &pQuality,
		&vehicleJob, &personJob, &tracksRaw)
	if err != nil {
		return e, err
	}
	if e.Tracks, err = parseTracks(tracksRaw); err != nil {
		return e, err
	}
	if vType != nil && vColor != nil && vQuality != nil && vTypeConf != nil && vColorConf != nil {
		e.Vehicle = &VehicleAttr{
			Type: *vType, TypeConfidence: *vTypeConf,
			Color: *vColor, ColorConfidence: *vColorConf, ColorQuality: *vQuality,
		}
		if trailer != nil {
			e.Vehicle.TrailerColor = *trailer
		}
		if trailerConf != nil {
			e.Vehicle.TrailerColorConfidence = *trailerConf
		}
	}
	if vehicleJob != nil {
		e.VehicleJob = *vehicleJob
	}
	if personJob != nil {
		e.PersonJob = *personJob
	}
	if upper != nil && lower != nil && pQuality != nil && upperConf != nil && lowerConf != nil {
		e.Person = &PersonAttr{
			UpperColor: *upper, UpperConfidence: *upperConf,
			LowerColor: *lower, LowerConfidence: *lowerConf, ColorQuality: *pQuality,
		}
	}
	return e, nil
}

// ListEvents searches the index newest first. Plates and sub_labels (which carry recognized
// plate text on LPR cameras) are only returned on cameras where the actor holds lpr.view, and
// only searchable on cameras where the actor holds lpr.search.
func (s *Service) ListEvents(ctx context.Context, actor authz.Actor, f Filter) (Page[Event], error) {
	var out Page[Event]
	n := limit(f.Limit)
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		perm := authz.EventsView
		if f.Plate != "" || len(f.Labels) > 0 || len(f.Zones) > 0 || len(f.SubLabels) > 0 || f.From != nil ||
			f.HasSnapshot != nil || f.HasPreview != nil || len(f.VehicleTypes) > 0 || len(f.VehicleColors) > 0 {
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
			// sub_label carries recognized plate text on LPR-capable cameras
			// (frigatemock.enrich); filtering by it there is gated the same as the Plate filter
			// above, requiring lpr.search (events.search alone is not enough). On a non-LPR
			// camera sub_label is an ordinary event field (e.g. a Frigate face-recognition name)
			// and follows only the normal events.search scoping already applied via `cams` above
			// (user decision 2026-09-28). Gating checks c.lpr (the camera's CURRENT flag) OR e.lpr
			// (this event's flag at ingestion time): keying on c.lpr alone let a camera switched
			// off LPR after ingestion unlock its historical events' sub_labels (SECURITY review
			// finding on commit 7658e50). Both are NOT NULL columns, so there is no "unknown"
			// camera to fail open on here.
			lprSearchCams, err := c.CameraIDs(ctx, authz.LPRSearch)
			if err != nil {
				return err
			}
			b.add("(NOT (c.lpr OR e.lpr) OR e.camera_id = ANY(?))", lprSearchCams)
			b.add("e.sub_labels && ?", f.SubLabels)
		}
		if f.Severity != "" {
			b.add("e.severity = ?", f.Severity)
		}
		addTimeFilter(&b, f)
		if f.Reviewed != nil {
			b.add("e.reviewed = ?", *f.Reviewed)
		}
		if f.HasSnapshot != nil {
			b.add("e.has_snapshot = ?", *f.HasSnapshot)
		}
		if f.HasPreview != nil {
			b.add("(e.preview_key <> '') = ?", *f.HasPreview)
		}
		if len(f.VehicleTypes) > 0 {
			b.add("va.vehicle_type = ANY(?)", f.VehicleTypes)
		}
		if len(f.VehicleColors) > 0 {
			b.add("va.vehicle_color = ANY(?)", f.VehicleColors)
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
				if e.CameraLPR || e.LPR {
					// sub_labels carry plate text on LPR-capable cameras, so they are redacted
					// there under the same lpr.view gate as plates. On a non-LPR camera sub_label
					// is not plate data (e.g. a face name), so it stays visible to any actor with
					// events.view (user decision 2026-09-28). Checking e.LPR (ingestion-time) as
					// well as e.CameraLPR (current) keeps this gated after the camera is switched
					// off LPR (SECURITY review finding on commit 7658e50).
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
		if cam.Lpr || e.LPR {
			// See ListEvents: sub_labels are plate data (and stay redacted) on LPR-capable
			// cameras; on other cameras they follow normal events.view (user decision 2026-09-28).
			// e.LPR (ingestion-time) keeps this gated even after the camera is switched off LPR
			// (SECURITY review finding on commit 7658e50).
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

// Reprocess queues color and classification again for one event. Historical
// alarms are not raised. The returned event already shows the job as pending.
func (s *Service) Reprocess(ctx context.Context, actor authz.Actor, id uuid.UUID) (Event, error) {
	var out Event
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		e, err := getEvent(ctx, tx, c, authz.EventsReview, id)
		if err != nil {
			return err
		}
		if err := vehicle.Requeue(ctx, tx, e.ID, e.TenantID, e.Labels); err != nil {
			return err
		}
		if err := vehicle.RequeuePerson(ctx, tx, e.ID, e.TenantID, e.Labels); err != nil {
			return err
		}
		out, err = getEvent(ctx, tx, c, authz.EventsReview, id)
		return err
	})
	return out, err
}

// MaxBulkReview bounds one SetReviewedBulk call.
const MaxBulkReview = 200

// SetReviewedBulk applies SetReviewed to many events in ONE transaction: every id must exist and
// pass the same events.review check as the single path, otherwise nothing changes and the first
// failure (forbidden or not found) is returned. Duplicate ids are collapsed; results keep the
// order of first appearance.
func (s *Service) SetReviewedBulk(ctx context.Context, actor authz.Actor, ids []uuid.UUID, reviewed bool) ([]Event, error) {
	if len(ids) == 0 || len(ids) > MaxBulkReview {
		return nil, &inventory.ValidationError{Msg: fmt.Sprintf("ids must hold between 1 and %d events", MaxBulkReview)}
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	out := make([]Event, 0, len(unique))
	err := s.tx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		out = out[:0]
		for _, id := range unique {
			e, err := getEvent(ctx, tx, c, authz.EventsReview, id)
			if err != nil {
				return err
			}
			e.Reviewed = reviewed
			out = append(out, e)
		}
		_, err := tx.Exec(ctx, `UPDATE events SET reviewed = $2, updated_at = now() WHERE id = ANY($1)`, unique, reviewed)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
	Vehicle *VehicleAttr
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
	VehicleTypes   []string
	VehicleColors  []string
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
		// Vehicle type and color live on the review row, not on lpr_reads. They are applied
		// after the page of reads is chosen, except when the caller is filtering by them.
		var vehicleWhere []string
		if len(f.VehicleTypes) > 0 {
			b.args = append(b.args, f.VehicleTypes)
			vehicleWhere = append(vehicleWhere, fmt.Sprintf("ev.vehicle_type = ANY($%d)", len(b.args)))
		}
		if len(f.VehicleColors) > 0 {
			b.args = append(b.args, f.VehicleColors)
			vehicleWhere = append(vehicleWhere, fmt.Sprintf("ev.vehicle_color = ANY($%d)", len(b.args)))
		}
		b.args = append(b.args, n+1)
		limitPh := len(b.args)
		// The detection_ids lookup is a per-row GIN probe. Running it before LIMIT walks
		// every historical read and is what left /plates and the map panel on "buscando".
		// MATERIALIZED forces Postgres to take the page (or the date window) first.
		pageSQL := `SELECT l.id, l.site_id, l.server_id, l.camera_id, l.remote_event_id, l.plate, l.plate_normalized,
       l.score, l.label, l.zones, l.seen_at
FROM lpr_reads l
WHERE ` + b.sql()
		const plateCols = `p.id, p.site_id, s.name, p.server_id, fs.name, p.camera_id, c.display_name, p.plate, p.plate_normalized,
       p.score, p.label, p.zones, p.seen_at,
       ev.event_id, ev.vehicle_type, ev.vehicle_type_confidence, ev.vehicle_color, ev.vehicle_color_confidence, ev.color_quality,
       ev.trailer_color, ev.trailer_color_confidence`
		const plateJoins = `JOIN cameras c ON c.id = p.camera_id
JOIN sites s ON s.id = p.site_id
JOIN frigate_servers fs ON fs.id = p.server_id
LEFT JOIN LATERAL (
    SELECT e.id AS event_id, va.vehicle_type, va.vehicle_type_confidence,
           va.vehicle_color, va.vehicle_color_confidence, va.color_quality,
           va.trailer_color, va.trailer_color_confidence
    FROM events e
    LEFT JOIN vehicle_attributes va ON va.event_id = e.id
    WHERE e.server_id = p.server_id AND e.detection_ids @> ARRAY[p.remote_event_id]
    ORDER BY e.start_time DESC
    LIMIT 1
) ev ON true`
		var q string
		if len(vehicleWhere) == 0 {
			q = `WITH page AS MATERIALIZED (
` + pageSQL + fmt.Sprintf(`
    ORDER BY l.seen_at DESC, l.id DESC
    LIMIT $%d
)`, limitPh) + `
SELECT ` + plateCols + `
FROM page p
` + plateJoins + `
ORDER BY p.seen_at DESC, p.id DESC`
		} else {
			q = `WITH scoped AS MATERIALIZED (
` + pageSQL + `
)
SELECT ` + plateCols + `
FROM scoped p
` + plateJoins + `
WHERE ` + strings.Join(vehicleWhere, " AND ") + fmt.Sprintf(`
ORDER BY p.seen_at DESC, p.id DESC
LIMIT $%d`, limitPh)
		}
		rows, err := tx.Query(ctx, q, b.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r PlateRead
			var vType, vColor, vQuality, trailer *string
			var vTypeConf, vColorConf, trailerConf *float32
			if err := rows.Scan(&r.ID, &r.SiteID, &r.SiteName, &r.ServerID, &r.ServerName, &r.CameraID, &r.CameraName,
				&r.Plate, &r.Normalized, &r.Score, &r.Label, &r.Zones, &r.SeenAt, &r.EventID,
				&vType, &vTypeConf, &vColor, &vColorConf, &vQuality, &trailer, &trailerConf); err != nil {
				return err
			}
			if vType != nil && vColor != nil && vQuality != nil && vTypeConf != nil && vColorConf != nil {
				r.Vehicle = &VehicleAttr{
					Type: *vType, TypeConfidence: *vTypeConf,
					Color: *vColor, ColorConfidence: *vColorConf, ColorQuality: *vQuality,
				}
				if trailer != nil {
					r.Vehicle.TrailerColor = *trailer
				}
				if trailerConf != nil {
					r.Vehicle.TrailerColorConfidence = *trailerConf
				}
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
