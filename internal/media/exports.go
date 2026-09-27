package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type Export struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	SiteID      uuid.UUID
	ServerID    uuid.UUID
	CameraID    uuid.UUID
	CameraName  string
	RequestedBy uuid.UUID
	Requester   string
	Name        string
	Start       time.Time
	End         time.Time
	Status      string
	Progress    float32
	Error       string
	RemotePath  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const exportColumns = `x.id, x.tenant_id, x.site_id, x.server_id, x.camera_id, c.display_name, x.requested_by, u.username,
x.name, x.start_time, x.end_time, x.status, x.progress, x.error, x.remote_path, x.created_at, x.updated_at`

const exportJoins = `FROM exports x JOIN cameras c ON c.id = x.camera_id JOIN users u ON u.id = x.requested_by`

func scanExport(row pgx.Row) (Export, error) {
	var e Export
	err := row.Scan(&e.ID, &e.TenantID, &e.SiteID, &e.ServerID, &e.CameraID, &e.CameraName, &e.RequestedBy, &e.Requester,
		&e.Name, &e.Start, &e.End, &e.Status, &e.Progress, &e.Error, &e.RemotePath, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// MaxExportLength bounds one export (PRD §52): long ranges belong in several exports.
const MaxExportLength = 2 * time.Hour

type ExportInput struct {
	CameraID uuid.UUID
	Start    time.Time
	End      time.Time
	Name     string
}

// CreateExport asks the camera's Frigate to export a clip (exports.create). The file stays
// in that Frigate; the VMS tracks it and relays the download.
func (s *Service) CreateExport(ctx context.Context, actor authz.Actor, in ExportInput) (Export, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case !in.End.After(in.Start):
		return Export{}, invalid("end must be after start")
	case in.End.Sub(in.Start) > MaxExportLength:
		return Export{}, invalid("an export can be at most 2 hours long")
	case in.End.After(time.Now().Add(time.Minute)):
		return Export{}, invalid("the range cannot end in the future")
	case len(in.Name) > 200:
		return Export{}, invalid("name is too long")
	}
	cam, err := s.Authorize(ctx, actor, in.CameraID, authz.ExportsCreate)
	if err != nil {
		return Export{}, err
	}
	if in.Name == "" {
		in.Name = fmt.Sprintf("%s %s", cam.RemoteName, in.Start.Local().Format("2006-01-02 15:04"))
	}
	a, err := s.Adapters.Get(ctx, cam.Server)
	if err != nil {
		return Export{}, &inventory.FrigateError{Err: err}
	}
	remoteID, err := a.StartExport(ctx, cam.RemoteName, float64(in.Start.Unix()), float64(in.End.Unix()), in.Name)
	if err != nil {
		return Export{}, &inventory.FrigateError{Err: err}
	}
	var id uuid.UUID
	err = s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		if err := tx.QueryRow(ctx, `
INSERT INTO exports (tenant_id, site_id, server_id, camera_id, requested_by, name, start_time, end_time, remote_id, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'running') RETURNING id`,
			cam.TenantID, cam.SiteID, cam.Server.ID, cam.ID, actor.UserID, in.Name, in.Start, in.End, remoteID).Scan(&id); err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{"camera_id": cam.ID, "start": in.Start, "end": in.End, "name": in.Name})
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &cam.TenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionExportCreated,
			TargetType: "export", TargetID: &id, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		})
	})
	if err != nil {
		return Export{}, err
	}
	return s.GetExport(ctx, actor, id)
}

// ListExports shows the caller's exports, plus every export of cameras where they hold
// exports.download.
func (s *Service) ListExports(ctx context.Context, actor authz.Actor) ([]Export, error) {
	var out []Export
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		cams, err := c.CameraIDs(ctx, authz.ExportsDownload)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+exportColumns+` `+exportJoins+`
WHERE x.deleted_at IS NULL AND (x.requested_by = $1 OR x.camera_id = ANY($2))
ORDER BY x.created_at DESC LIMIT 500`, actor.UserID, cams)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanExport(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Export{}
	}
	return out, err
}

func (s *Service) GetExport(ctx context.Context, actor authz.Actor, id uuid.UUID) (Export, error) {
	var out Export
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		e, err := scanExport(tx.QueryRow(ctx, `SELECT `+exportColumns+` `+exportJoins+` WHERE x.id = $1 AND x.deleted_at IS NULL`, id))
		if err != nil {
			return store.Classify(err)
		}
		if e.RequestedBy != actor.UserID {
			cam, err := db.New(tx).GetCamera(ctx, e.CameraID)
			if err != nil {
				return store.Classify(err)
			}
			if err := c.Require(authz.ExportsDownload, access.Camera(cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, cam.GroupIds)); err != nil {
				return err
			}
		}
		out = e
		return nil
	})
	return out, err
}

// DeleteExport forgets the export in the VMS (exports.delete). The file in Frigate is
// left alone: Frigate's retention and its own UI manage it.
func (s *Service) DeleteExport(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	e, err := s.GetExport(ctx, actor, id)
	if err != nil {
		return err
	}
	if _, err := s.Authorize(ctx, actor, e.CameraID, authz.ExportsDelete); err != nil && e.RequestedBy != actor.UserID {
		return err
	}
	return s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		_, err := tx.Exec(ctx, `UPDATE exports SET deleted_at = now() WHERE id = $1`, id)
		return err
	})
}

// ExportTracker follows running exports in Frigate until they finish (runs in the worker).
type ExportTracker struct {
	Store    *store.Store
	Adapters *inventory.Adapters
	Interval time.Duration
	Log      interface {
		WarnContext(ctx context.Context, msg string, args ...any)
	}
}

func (t *ExportTracker) Run(ctx context.Context) {
	tick := time.NewTicker(t.Interval)
	defer tick.Stop()
	for {
		t.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Exports that Frigate never reports back are failed after this long.
const exportTimeout = time.Hour

func (t *ExportTracker) Once(ctx context.Context) {
	type row struct {
		id       uuid.UUID
		remoteID string
		created  time.Time
		server   db.FrigateServer
	}
	var active []row
	err := t.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, remote_id, server_id, created_at FROM exports
WHERE status IN ('pending', 'running') AND deleted_at IS NULL ORDER BY created_at LIMIT 100`)
		if err != nil {
			return err
		}
		var ids []row
		var serverIDs []uuid.UUID
		for rows.Next() {
			var r row
			var sid uuid.UUID
			if err := rows.Scan(&r.id, &r.remoteID, &sid, &r.created); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, r)
			serverIDs = append(serverIDs, sid)
		}
		rows.Close()
		q := db.New(tx)
		for i := range ids {
			srv, err := q.GetServerRow(ctx, serverIDs[i])
			if err != nil {
				continue // server removed: the export stays as it is
			}
			ids[i].server = srv
			active = append(active, ids[i])
		}
		return rows.Err()
	})
	if err != nil {
		t.Log.WarnContext(ctx, "exports: list active", "error", err)
		return
	}
	for _, r := range active {
		status, progress, errMsg, path := "running", float32(0), "", ""
		a, err := t.Adapters.Get(ctx, r.server)
		if err == nil {
			var info frigate.ExportInfo
			info, err = a.Export(ctx, r.remoteID)
			switch {
			case err == nil && info.Failed:
				status, errMsg = "failed", info.Error
			case err == nil && !info.InProgress && info.VideoPath != "":
				status, progress, path = "ready", 100, info.VideoPath
			case err == nil:
				progress = float32(info.Progress)
			case errors.Is(err, frigate.ErrNotFound) && time.Since(r.created) > 2*time.Minute:
				status, errMsg = "failed", "Frigate no longer has this export"
			}
		}
		if status == "running" && time.Since(r.created) > exportTimeout {
			status, errMsg = "failed", "timed out waiting for Frigate"
		}
		uerr := t.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE exports SET status = $2, progress = $3, error = $4, remote_path = $5, updated_at = now() WHERE id = $1`,
				r.id, status, progress, errMsg, path)
			return err
		})
		if uerr != nil {
			t.Log.WarnContext(ctx, "exports: update", "export_id", r.id, "error", uerr)
		}
	}
}
