package media

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Cell is one tile of a view. A nil cell is an empty tile.
type Cell struct {
	CameraID uuid.UUID `json:"camera_id"`
	Quality  string    `json:"quality,omitempty"`
}

type Layout struct {
	Columns int     `json:"columns"`
	Cells   []*Cell `json:"cells"`
}

type View struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	OwnerID   uuid.UUID
	Name      string
	Shared    bool
	Layout    Layout
	CreatedAt time.Time
	UpdatedAt time.Time
	// Editable reports whether the caller may change or delete the view.
	Editable bool
}

type ViewInput struct {
	TenantID *uuid.UUID
	Name     string
	Shared   bool
	Layout   Layout
}

const maxCells = 64

func invalid(msg string) error { return &inventory.ValidationError{Msg: msg} }

func (in *ViewInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return invalid("name is required (max 200 characters)")
	}
	if in.Layout.Columns < 1 || in.Layout.Columns > 8 {
		return invalid("columns must be between 1 and 8")
	}
	if len(in.Layout.Cells) > maxCells {
		return invalid("a view has at most 64 tiles")
	}
	for _, c := range in.Layout.Cells {
		if c != nil && c.Quality != "" && c.Quality != "sub" && c.Quality != "main" {
			return invalid("quality must be sub or main")
		}
	}
	return nil
}

func scanView(row pgx.Row) (View, error) {
	var v View
	var raw []byte
	err := row.Scan(&v.ID, &v.TenantID, &v.OwnerID, &v.Name, &v.Shared, &raw, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v.Layout)
	return v, err
}

const viewColumns = `id, tenant_id, owner_id, name, shared, layout, created_at, updated_at`

func (s *Service) rawTx(ctx context.Context, actor authz.Actor, fn func(tx pgx.Tx, c *access.Checker) error) error {
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		c, err := access.Load(ctx, db.New(tx), actor)
		if err != nil {
			return err
		}
		return fn(tx, c)
	})
}

func canEdit(actor authz.Actor, c *access.Checker, v View) bool {
	if v.OwnerID == actor.UserID {
		return true
	}
	return v.Shared && c.Can(authz.ViewsManageShared, access.Tenant(v.TenantID))
}

// redact hides cameras the caller cannot watch live, so a shared view never reveals
// cameras outside the caller's permissions (PRD §31). Their tiles become empty.
func redact(v *View, visible map[uuid.UUID]bool) {
	for i, c := range v.Layout.Cells {
		if c != nil && !visible[c.CameraID] {
			v.Layout.Cells[i] = nil
		}
	}
}

func liveCameras(ctx context.Context, c *access.Checker) (map[uuid.UUID]bool, error) {
	ids, err := c.CameraIDs(ctx, authz.LiveView)
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m, nil
}

// ListViews returns the caller's own views and the shared views of their tenant.
func (s *Service) ListViews(ctx context.Context, actor authz.Actor) ([]View, error) {
	var out []View
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		visible, err := liveCameras(ctx, c)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+viewColumns+` FROM views
WHERE deleted_at IS NULL AND (owner_id = $1 OR shared) ORDER BY shared, lower(name)`, actor.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanView(rows)
			if err != nil {
				return err
			}
			if v.OwnerID != actor.UserID && !actor.IsPlatform() && v.TenantID != *actor.TenantID {
				continue
			}
			v.Editable = canEdit(actor, c, v)
			redact(&v, visible)
			out = append(out, v)
		}
		return rows.Err()
	})
	if out == nil {
		out = []View{}
	}
	return out, err
}

func getView(ctx context.Context, tx pgx.Tx, actor authz.Actor, id uuid.UUID) (View, error) {
	v, err := scanView(tx.QueryRow(ctx, `SELECT `+viewColumns+` FROM views WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return v, store.Classify(err)
	}
	if v.OwnerID != actor.UserID && !v.Shared {
		return v, store.ErrNotFound // another user's private view does not exist for the caller
	}
	return v, nil
}

func (s *Service) GetView(ctx context.Context, actor authz.Actor, id uuid.UUID) (View, error) {
	var out View
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		v, err := getView(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		visible, err := liveCameras(ctx, c)
		if err != nil {
			return err
		}
		v.Editable = canEdit(actor, c, v)
		redact(&v, visible)
		out = v
		return nil
	})
	return out, err
}

// checkCells refuses cameras the caller cannot watch: nobody can put a camera they do
// not see into a view, shared or not.
func checkCells(ctx context.Context, c *access.Checker, l Layout) error {
	visible, err := liveCameras(ctx, c)
	if err != nil {
		return err
	}
	for _, cell := range l.Cells {
		if cell != nil && !visible[cell.CameraID] {
			return invalid("the view contains a camera you cannot watch")
		}
	}
	return nil
}

func (s *Service) viewTenant(actor authz.Actor, in *ViewInput) (uuid.UUID, error) {
	if !actor.IsPlatform() {
		if in.TenantID != nil && *in.TenantID != *actor.TenantID {
			return uuid.Nil, store.ErrNotFound
		}
		return *actor.TenantID, nil
	}
	if in.TenantID == nil {
		return uuid.Nil, invalid("tenant_id is required for platform users")
	}
	return *in.TenantID, nil
}

func (s *Service) CreateView(ctx context.Context, actor authz.Actor, in ViewInput) (View, error) {
	if err := in.validate(); err != nil {
		return View{}, err
	}
	tenantID, err := s.viewTenant(actor, &in)
	if err != nil {
		return View{}, err
	}
	var id uuid.UUID
	err = s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		perm := authz.ViewsCreatePrivate
		if in.Shared {
			perm = authz.ViewsCreateShared
		}
		if err := c.Require(perm, access.Tenant(tenantID)); err != nil {
			return err
		}
		if err := checkCells(ctx, c, in.Layout); err != nil {
			return err
		}
		raw, _ := json.Marshal(in.Layout)
		return tx.QueryRow(ctx, `INSERT INTO views (tenant_id, owner_id, name, shared, layout) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			tenantID, actor.UserID, in.Name, in.Shared, raw).Scan(&id)
	})
	if err != nil {
		return View{}, err
	}
	return s.GetView(ctx, actor, id)
}

func (s *Service) ReplaceView(ctx context.Context, actor authz.Actor, id uuid.UUID, in ViewInput) (View, error) {
	if err := in.validate(); err != nil {
		return View{}, err
	}
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		v, err := getView(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canEdit(actor, c, v) {
			return access.ErrForbidden
		}
		if in.Shared && !v.Shared {
			if err := c.Require(authz.ViewsCreateShared, access.Tenant(v.TenantID)); err != nil {
				return err
			}
		}
		// Tiles the editor cannot see are kept as they were instead of being dropped.
		visible, err := liveCameras(ctx, c)
		if err != nil {
			return err
		}
		merged := in.Layout
		for i := range merged.Cells {
			if merged.Cells[i] == nil && i < len(v.Layout.Cells) && v.Layout.Cells[i] != nil && !visible[v.Layout.Cells[i].CameraID] {
				merged.Cells[i] = v.Layout.Cells[i]
			} else if merged.Cells[i] != nil && !visible[merged.Cells[i].CameraID] {
				return invalid("the view contains a camera you cannot watch")
			}
		}
		raw, _ := json.Marshal(merged)
		_, err = tx.Exec(ctx, `UPDATE views SET name = $2, shared = $3, layout = $4, updated_at = now() WHERE id = $1`, id, in.Name, in.Shared, raw)
		return err
	})
	if err != nil {
		return View{}, err
	}
	return s.GetView(ctx, actor, id)
}

func (s *Service) DeleteView(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		v, err := getView(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canEdit(actor, c, v) {
			return access.ErrForbidden
		}
		_, err = tx.Exec(ctx, `UPDATE views SET deleted_at = now() WHERE id = $1`, id)
		return err
	})
}

// Recordings lists continuous recording spans of a camera (recordings.seek) for the
// timeline, asking its Frigate.
func (s *Service) Recordings(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, from, to time.Time) ([]RecordingSpan, error) {
	if !to.After(from) || to.Sub(from) > 7*24*time.Hour {
		return nil, invalid("the range must be positive and at most 7 days")
	}
	cam, err := s.Authorize(ctx, actor, cameraID, authz.RecordingsSeek)
	if err != nil {
		return nil, err
	}
	a, err := s.Adapters.Get(ctx, cam.Server)
	if err != nil {
		return nil, &inventory.FrigateError{Err: err}
	}
	segs, err := a.Recordings(ctx, cam.RemoteName, float64(from.Unix()), float64(to.Unix()))
	if err != nil {
		return nil, &inventory.FrigateError{Err: err}
	}
	out := make([]RecordingSpan, 0, len(segs))
	for _, sg := range segs {
		out = append(out, RecordingSpan{
			Start: time.UnixMilli(int64(sg.StartTime * 1000)).UTC(), End: time.UnixMilli(int64(sg.EndTime * 1000)).UTC(),
			Motion: sg.Motion, Objects: sg.Objects,
		})
	}
	return out, nil
}

type RecordingSpan struct {
	Start, End      time.Time
	Motion, Objects int
}
