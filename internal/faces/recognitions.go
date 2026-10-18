package faces

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ErrInvalidCursor is returned for a pagination cursor that was not produced by this API.
var ErrInvalidCursor = errors.New("invalid cursor")

const (
	defaultRecognitionLimit = 50
	maxRecognitionLimit     = 200
)

// RecognitionFilter narrows the recognition timeline. Empty slices mean "no restriction".
type RecognitionFilter struct {
	PersonIDs []uuid.UUID
	CameraIDs []uuid.UUID
	From, To  *time.Time
	Cursor    string
	Limit     int
}

// Recognition is one sighting: an event whose sub_labels carry a registered person's name.
// An event that recognized two persons yields two rows.
type Recognition struct {
	EventID      uuid.UUID
	PersonID     uuid.UUID
	PersonName   string
	CameraID     uuid.UUID
	CameraName   string
	SiteID       uuid.UUID
	SiteName     string
	ServerName   string
	StartTime    time.Time
	EndTime      *time.Time
	HasThumbnail bool
	HasSnapshot  bool
}

type RecognitionPage struct {
	Items []Recognition
	Next  string
}

func encodeRecognitionCursor(r Recognition) string {
	raw := strconv.FormatInt(r.StartTime.UnixMicro(), 10) + "|" + r.EventID.String() + "|" + r.PersonID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeRecognitionCursor(c string) (time.Time, uuid.UUID, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.Split(string(raw), "|")
	if err != nil || len(parts) != 3 {
		return time.Time{}, uuid.Nil, uuid.Nil, ErrInvalidCursor
	}
	micros, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, uuid.Nil, ErrInvalidCursor
	}
	ev, err1 := uuid.Parse(parts[1])
	pe, err2 := uuid.Parse(parts[2])
	if err1 != nil || err2 != nil {
		return time.Time{}, uuid.Nil, uuid.Nil, ErrInvalidCursor
	}
	return time.UnixMicro(micros).UTC(), ev, pe, nil
}

// ListRecognitions is the recognition timeline (faces.view), newest first with keyset
// pagination. Events are limited to the cameras the actor holds events.view on, exactly like
// the events API, so a person can never be tracked through cameras the caller cannot see. Only
// live persons appear: the sightings of a deleted person are scrubbed.
func (s *Service) ListRecognitions(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, f RecognitionFilter) (RecognitionPage, error) {
	var out RecognitionPage
	if err := authorizeTenant(actor, tenantID); err != nil {
		return out, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultRecognitionLimit
	}
	limit = min(limit, maxRecognitionLimit)
	var curTime time.Time
	var curEvent, curPerson uuid.UUID
	if f.Cursor != "" {
		var err error
		if curTime, curEvent, curPerson, err = decodeRecognitionCursor(f.Cursor); err != nil {
			return out, err
		}
	}
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := c.Require(authz.FacesView, access.Tenant(tenantID)); err != nil {
			return err
		}
		cams, err := c.CameraIDs(ctx, authz.EventsView)
		if err != nil {
			return err
		}
		args := []any{tenantID, cams}
		where := []string{"e.tenant_id = $1", "e.camera_id = ANY($2)"}
		add := func(cond string, v any) {
			args = append(args, v)
			where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
		}
		if len(f.PersonIDs) > 0 {
			add("p.id = ANY(?)", f.PersonIDs)
		}
		if len(f.CameraIDs) > 0 {
			add("e.camera_id = ANY(?)", f.CameraIDs)
		}
		if f.From != nil {
			add("e.start_time >= ?", *f.From)
		}
		if f.To != nil {
			add("e.start_time < ?", *f.To)
		}
		if f.Cursor != "" {
			args = append(args, curTime, curEvent, curPerson)
			n := len(args)
			where = append(where, fmt.Sprintf("(e.start_time, e.id, p.id) < ($%d, $%d, $%d)", n-2, n-1, n))
		}
		args = append(args, limit+1)
		rows, err := tx.Query(ctx, `
SELECT e.id, p.id, p.display_name, e.camera_id, c.display_name, e.site_id, s.name, fs.name,
       e.start_time, e.end_time, e.thumb_key <> '', e.has_snapshot
FROM events e
JOIN face_persons p ON p.tenant_id = e.tenant_id AND p.deleted_at IS NULL AND p.frigate_name = ANY(e.sub_labels)
JOIN cameras c ON c.id = e.camera_id
JOIN sites s ON s.id = e.site_id
JOIN frigate_servers fs ON fs.id = e.server_id
WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(`
ORDER BY e.start_time DESC, e.id DESC, p.id DESC
LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Recognition
			if err := rows.Scan(&r.EventID, &r.PersonID, &r.PersonName, &r.CameraID, &r.CameraName, &r.SiteID, &r.SiteName,
				&r.ServerName, &r.StartTime, &r.EndTime, &r.HasThumbnail, &r.HasSnapshot); err != nil {
				return err
			}
			out.Items = append(out.Items, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		var target uuid.UUID
		if len(f.PersonIDs) == 1 {
			target = f.PersonIDs[0]
		}
		return s.audit(ctx, q, actor, tenantID, ActionRecognitionsRead, "face_person", target,
			map[string]any{"count": min(len(out.Items), limit), "persons": len(f.PersonIDs), "cameras": len(f.CameraIDs)})
	})
	if err != nil {
		return RecognitionPage{}, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = encodeRecognitionCursor(out.Items[limit-1])
	}
	if out.Items == nil {
		out.Items = []Recognition{}
	}
	return out, nil
}
