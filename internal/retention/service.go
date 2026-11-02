package retention

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/hoststat"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionUpdated is the audit action written when an operator changes the retention.
const ActionUpdated = "RETENTION_UPDATED"

// Service is the operator-facing side of the retention: read, preview and change the platform setting.
// Every method needs the platform-level tenant.manage permission, because the setting is global and
// deletes data of every tenant.
type Service struct {
	Store *store.Store
	// VolumePath is the filesystem holding the stored media. It must be the path the worker measures
	// (RETENTION_VOLUME_PATH) or the API and the cleanup would disagree about how full the volume is.
	VolumePath string
	Now        func() time.Time
}

// Volume is how full the data volume is. Measured is false when the path cannot be read.
type Volume struct {
	Measured   bool
	TotalBytes uint64
	FreeBytes  uint64
	Percent    float64
}

// Stored is what a class holds now.
type Stored struct {
	Records int64
	Bytes   int64
}

// View is the setting plus what the operator needs to judge it.
type View struct {
	Settings Settings
	Volume   Volume
	// FacesPending is every unnamed face still stored.
	FacesPending Stored
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) gate(ctx context.Context, q *db.Queries, actor authz.Actor) error {
	chk, err := access.Load(ctx, q, actor)
	if err != nil {
		return err
	}
	return chk.Require(authz.TenantManage, authz.Resource{Kind: authz.ScopePlatform})
}

func (s *Service) volume() Volume {
	total, free, ok := hoststat.DiskUsage(s.VolumePath)
	if !ok {
		return Volume{}
	}
	free = min(free, total)
	return Volume{Measured: true, TotalBytes: total, FreeBytes: free, Percent: float64(total-free) / float64(total) * 100}
}

// Get returns the setting with the volume usage and the unnamed faces stored.
func (s *Service) Get(ctx context.Context, actor authz.Actor) (View, error) {
	var v View
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := s.gate(ctx, q, actor); err != nil {
			return err
		}
		row, err := q.GetRetentionSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		v.Settings = fromRow(row)
		pending, err := q.CountPendingFaceSightings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		v.FacesPending = Stored{Records: pending.Records, Bytes: pending.Bytes}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	v.Volume = s.volume()
	return v, nil
}

// Preview counts what the faces class would erase if it kept the given days; nothing is changed.
func (s *Service) Preview(ctx context.Context, actor authz.Actor, faceUnknownDays int) (Stored, error) {
	if err := (Settings{FaceUnknownDays: faceUnknownDays, PressurePercent: MinPressurePercent}).Validate(); err != nil {
		return Stored{}, err
	}
	var out Stored
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := s.gate(ctx, q, actor); err != nil {
			return err
		}
		if faceUnknownDays == ForeverDays {
			return nil
		}
		row, err := q.CountExpirableFaceSightings(ctx, s.now().Add(-time.Duration(faceUnknownDays)*24*time.Hour))
		if err != nil {
			return store.Classify(err)
		}
		out = Stored{Records: row.Records, Bytes: row.Bytes}
		return nil
	})
	return out, err
}

// Update validates and stores the setting and writes an audit entry with the old and new values.
func (s *Service) Update(ctx context.Context, actor authz.Actor, in Settings) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	var out Settings
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := s.gate(ctx, q, actor); err != nil {
			return err
		}
		before, err := q.GetRetentionSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		if out, err = save(ctx, q, in, actor.UserID); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]any{"before": snapshot(fromRow(before)), "after": snapshot(out)})
		if err != nil {
			return err
		}
		return q.InsertAudit(ctx, db.InsertAuditParams{
			ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionUpdated,
			TargetType: "retention_settings", Details: details, RequestID: logging.RequestID(ctx),
		})
	})
	return out, err
}

func snapshot(s Settings) map[string]any {
	return map[string]any{
		"face_unknown_days":          s.FaceUnknownDays,
		"notification_delivery_days": s.NotificationDeliveryDays,
		"notification_read_days":     s.NotificationReadDays,
		"pressure_cleanup":           s.PressureCleanup,
		"pressure_percent":           s.PressurePercent,
	}
}
