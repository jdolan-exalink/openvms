// Package clipwatermark implements PDW-4: an asynchronous job, processed by the worker,
// that burns the plate detail watermark (date/time + owner branding, see
// internal/watermark) into a plate read's clip via ffmpeg, the same UX as
// internal/media's Exports (queued -> running -> done/failed, polled, then downloaded).
// The finished file lives in the object store, not in Frigate.
package clipwatermark

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/watermark"
)

// Audit actions (PRD §66 naming convention).
const (
	ActionClipWatermarkRequested = "CLIP_WATERMARK_REQUESTED"
	ActionClipDownloaded         = "CLIP_DOWNLOADED"
)

// ErrNotReady is returned by Download when the job has not finished yet.
var ErrNotReady = errors.New("clip watermark job is not ready")

// Blobs stores clip and logo bytes; *objectstore.Store implements it.
type Blobs interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
}

type Service struct {
	Store    *store.Store
	Media    *media.Service
	Branding *branding.Service
	Blobs    Blobs
	Log      *slog.Logger
}

// Job is a clip watermark job as the API exposes it.
type Job struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	CameraID  uuid.UUID
	LPRReadID uuid.UUID
	Status    string
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func toJob(r db.ClipWatermarkJob) Job {
	return Job{
		ID: r.ID, TenantID: r.TenantID, CameraID: r.CameraID, LPRReadID: r.LprReadID,
		Status: r.Status, Error: r.Error, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// authorizeClip loads the lpr_reads row behind readID and authorizes recordings.view +
// lpr.view (the same rule the clip *view* endpoint uses, internal/media/gateway.go
// lprReadCamera) plus every permission in extra (e.g. exports.create, exports.download).
// Another tenant's read answers store.ErrNotFound (via media.Service.Authorize); a missing
// permission answers access.ErrForbidden, which the API layer audits centrally.
func (s *Service) authorizeClip(ctx context.Context, actor authz.Actor, readID uuid.UUID, extra ...authz.Permission) (lprRead, media.Camera, error) {
	lr, err := s.lookupRead(ctx, actor, readID)
	if err != nil {
		return lprRead{}, media.Camera{}, err
	}
	cam, err := s.Media.Authorize(ctx, actor, lr.CameraID, authz.RecordingsView)
	if err != nil {
		return lprRead{}, media.Camera{}, err
	}
	for _, p := range append([]authz.Permission{authz.LPRView}, extra...) {
		if _, err := s.Media.Authorize(ctx, actor, lr.CameraID, p); err != nil {
			return lprRead{}, media.Camera{}, err
		}
	}
	return lr, cam, nil
}

type lprRead struct {
	ID            uuid.UUID
	CameraID      uuid.UUID
	RemoteEventID string
	SeenAt        time.Time
}

func (s *Service) lookupRead(ctx context.Context, actor authz.Actor, readID uuid.UUID) (lprRead, error) {
	out := lprRead{ID: readID}
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT camera_id, remote_event_id, seen_at FROM lpr_reads WHERE id = $1`, readID).
			Scan(&out.CameraID, &out.RemoteEventID, &out.SeenAt)
	})
	if err != nil {
		return lprRead{}, store.Classify(err)
	}
	return out, nil
}

// CreateJob starts a clip watermark job for readID (exports.create, in addition to the view
// permissions authorizeClip already checks). The tenant's current branding is frozen into
// the job (watermark text, and a copy of the logo if one is configured) at this moment, so
// the burned-in result matches what the requester saw when they asked for it, even if
// branding changes or the job sits queued for a while before the worker picks it up.
func (s *Service) CreateJob(ctx context.Context, actor authz.Actor, readID uuid.UUID) (Job, error) {
	lr, cam, err := s.authorizeClip(ctx, actor, readID, authz.ExportsCreate)
	if err != nil {
		return Job{}, err
	}

	b, err := s.Branding.Get(ctx, actor, cam.TenantID)
	if err != nil {
		s.Log.WarnContext(ctx, "load branding for clip watermark job", "error", err)
		b = branding.Branding{}
	}
	text := watermark.Text(lr.SeenAt, b.OwnerName)

	var row db.ClipWatermarkJob
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.InsertClipWatermarkJob(ctx, db.InsertClipWatermarkJobParams{
			TenantID: cam.TenantID, SiteID: cam.SiteID, ServerID: cam.Server.ID, CameraID: cam.ID,
			LprReadID: lr.ID, RemoteEventID: lr.RemoteEventID, RequestedBy: actor.UserID,
			WatermarkText: text, LogoKey: "",
		})
		if err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]any{"lpr_read_id": lr.ID})
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &cam.TenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionClipWatermarkRequested,
			TargetType: "clip_watermark_job", TargetID: &row.ID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: details,
		})
	})
	if err != nil {
		return Job{}, err
	}

	if b.HasLogo {
		if data, contentType, err := s.Branding.Logo(ctx, actor, cam.TenantID); err != nil {
			s.Log.WarnContext(ctx, "load branding logo for clip watermark job", "error", err)
		} else {
			key := jobLogoKey(row.ID)
			if err := s.Blobs.Put(ctx, key, data, contentType); err != nil {
				s.Log.WarnContext(ctx, "copy branding logo for clip watermark job", "error", err)
			} else if err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
				return q.SetClipWatermarkJobLogoKey(ctx, db.SetClipWatermarkJobLogoKeyParams{ID: row.ID, LogoKey: key})
			}); err != nil {
				s.Log.WarnContext(ctx, "save clip watermark job logo key", "error", err)
			} else {
				row.LogoKey = key
			}
		}
	}
	return toJob(row), nil
}

func (s *Service) getRow(ctx context.Context, actor authz.Actor, readID, jobID uuid.UUID) (db.ClipWatermarkJob, error) {
	var row db.ClipWatermarkJob
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.GetClipWatermarkJob(ctx, jobID)
		return err
	})
	if err != nil {
		return db.ClipWatermarkJob{}, store.Classify(err)
	}
	if row.LprReadID != readID {
		return db.ClipWatermarkJob{}, store.ErrNotFound
	}
	return row, nil
}

// GetJob polls a job's status (same view permissions as CreateJob, minus exports.create).
func (s *Service) GetJob(ctx context.Context, actor authz.Actor, readID, jobID uuid.UUID) (Job, error) {
	if _, _, err := s.authorizeClip(ctx, actor, readID); err != nil {
		return Job{}, err
	}
	row, err := s.getRow(ctx, actor, readID, jobID)
	if err != nil {
		return Job{}, err
	}
	return toJob(row), nil
}

// Download returns the finished, watermarked clip (exports.download, in addition to the
// view permissions), auditing CLIP_DOWNLOADED. ErrNotReady is returned while the job is
// still queued/running or if it failed.
func (s *Service) Download(ctx context.Context, actor authz.Actor, readID, jobID uuid.UUID) ([]byte, error) {
	if _, _, err := s.authorizeClip(ctx, actor, readID, authz.ExportsDownload); err != nil {
		return nil, err
	}
	row, err := s.getRow(ctx, actor, readID, jobID)
	if err != nil {
		return nil, err
	}
	if row.Status != "done" {
		return nil, ErrNotReady
	}
	data, _, err := s.Blobs.Get(ctx, row.OutputKey)
	if err != nil {
		return nil, err
	}
	err = s.Store.Tx(context.WithoutCancel(ctx), store.ScopeFor(actor), func(q *db.Queries) error {
		details, _ := json.Marshal(map[string]any{"lpr_read_id": readID})
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &row.TenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionClipDownloaded,
			TargetType: "clip_watermark_job", TargetID: &jobID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: details,
		})
	})
	if err != nil {
		s.Log.WarnContext(ctx, "audit clip download", "error", err)
	}
	return data, nil
}
