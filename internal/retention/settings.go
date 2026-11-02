// Package retention holds the platform-wide data retention settings and the job that enforces them.
package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	// MaxDays is the longest retention that can be set (ten years).
	MaxDays = 3650
	// ForeverDays marks a class that is never purged.
	ForeverDays = 0
	// MinPressurePercent and MaxPressurePercent bound the volume usage that triggers an early cleanup.
	MinPressurePercent = 50
	MaxPressurePercent = 99
	// pressureForeverDays is the retention assumed for a class kept forever when the volume is under pressure.
	pressureForeverDays = 30
)

// ErrInvalid is wrapped by every validation failure so callers can answer 400.
var ErrInvalid = errors.New("invalid retention settings")

// Settings is the platform-wide retention. Days: 0 keeps forever. A nil notification value means the
// environment default (NOTIFY_DELIVERY_RETENTION, NOTIFY_READ_RETENTION) applies.
type Settings struct {
	FaceUnknownDays          int
	NotificationDeliveryDays *int
	NotificationReadDays     *int
	PressureCleanup          bool
	PressurePercent          int
	UpdatedAt                time.Time
	UpdatedBy                *uuid.UUID
}

func fromRow(r db.RetentionSetting) Settings {
	return Settings{
		FaceUnknownDays:          int(r.FaceUnknownDays),
		NotificationDeliveryDays: intPtr(r.NotificationDeliveryDays),
		NotificationReadDays:     intPtr(r.NotificationReadDays),
		PressureCleanup:          r.PressureCleanup,
		PressurePercent:          int(r.PressurePercent),
		UpdatedAt:                r.UpdatedAt,
		UpdatedBy:                r.UpdatedBy,
	}
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v) //nolint:gosec // validated to 0..MaxDays first
	return &n
}

// Validate checks every field and names the first one out of range.
func (s Settings) Validate() error {
	days := func(name string, v int) error {
		if v < 0 || v > MaxDays {
			return fmt.Errorf("%w: %s must be between 0 and %d days", ErrInvalid, name, MaxDays)
		}
		return nil
	}
	if err := days("face_unknown_days", s.FaceUnknownDays); err != nil {
		return err
	}
	if s.NotificationDeliveryDays != nil {
		if err := days("notification_delivery_days", *s.NotificationDeliveryDays); err != nil {
			return err
		}
	}
	if s.NotificationReadDays != nil {
		if err := days("notification_read_days", *s.NotificationReadDays); err != nil {
			return err
		}
	}
	if s.PressurePercent < MinPressurePercent || s.PressurePercent > MaxPressurePercent {
		return fmt.Errorf("%w: pressure_percent must be between %d and %d", ErrInvalid, MinPressurePercent, MaxPressurePercent)
	}
	return nil
}

// PressureDays is the retention applied to the cleanable classes while the volume is over its limit: half
// of the configured days, rounded up and never below one. A class kept forever is treated as 30 days.
func PressureDays(days int) int {
	if days == ForeverDays {
		days = pressureForeverDays
	}
	return max(1, (days+1)/2)
}

// NotificationRetention converts a stored value to the duration the notification worker takes: a nil
// value keeps the given default, 0 disables the purge (a negative duration) and n is n days.
func NotificationRetention(days *int, def time.Duration) time.Duration {
	switch {
	case days == nil:
		return def
	case *days == ForeverDays:
		return -1
	}
	return time.Duration(*days) * 24 * time.Hour
}

// Load reads the platform setting. The row is created by the migration, so it always exists.
func Load(ctx context.Context, s *store.Store) (Settings, error) {
	var out Settings
	err := s.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.GetRetentionSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		out = fromRow(row)
		return nil
	})
	return out, err
}

// Save validates and stores the setting, recording who changed it.
func Save(ctx context.Context, s *store.Store, in Settings, by uuid.UUID) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	var out Settings
	err := s.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.UpdateRetentionSettings(ctx, db.UpdateRetentionSettingsParams{
			FaceUnknownDays:          int32(in.FaceUnknownDays), //nolint:gosec // validated
			NotificationDeliveryDays: int32Ptr(in.NotificationDeliveryDays),
			NotificationReadDays:     int32Ptr(in.NotificationReadDays),
			PressureCleanup:          in.PressureCleanup,
			PressurePercent:          int32(in.PressurePercent), //nolint:gosec // validated
			UpdatedBy:                &by,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = fromRow(row)
		return nil
	})
	return out, err
}
