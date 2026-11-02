package retention

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jdolan-exalink/openvms/internal/platform/hoststat"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	defaultInterval = time.Hour
	defaultBatch    = 500
	// maxBatchesPerSweep bounds one sweep; the rest waits for the next tick.
	maxBatchesPerSweep = 200

	// ActionPurged is the audit action written when a sweep erased something.
	ActionPurged = "RETENTION_PURGED"
)

// Result describes one sweep.
type Result struct {
	Faces    int64
	Days     int
	Pressure bool
}

// Purger enforces the retention setting. It reads the setting on every sweep, so a change takes effect
// without a restart, and it deletes in bounded batches.
type Purger struct {
	Store    *store.Store
	Log      *slog.Logger
	Interval time.Duration
	Batch    int32
	// VolumePercent returns how full the OpenVMS volume is (0..100); ok is false when it cannot be measured.
	VolumePercent func() (percent float64, ok bool)
	Now           func() time.Time
}

func (p *Purger) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

func (p *Purger) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Run sweeps now and then every Interval until ctx ends.
func (p *Purger) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := p.Sweep(ctx); err != nil && ctx.Err() == nil {
			p.log().ErrorContext(ctx, "retention sweep failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sweep erases the unnamed faces older than the retention. Under volume pressure the retention is
// shortened (see PressureDays). It never touches faces an operator already named or rejected.
func (p *Purger) Sweep(ctx context.Context) (Result, error) {
	cfg, err := Load(ctx, p.Store)
	if err != nil {
		return Result{}, err
	}
	res := Result{Days: cfg.FaceUnknownDays}
	if cfg.PressureCleanup && p.VolumePercent != nil {
		if pct, ok := p.VolumePercent(); ok && pct >= float64(cfg.PressurePercent) {
			res.Pressure = true
			res.Days = PressureDays(cfg.FaceUnknownDays)
		}
	}
	if res.Days == ForeverDays {
		return res, nil
	}
	cutoff := p.now().Add(-time.Duration(res.Days) * 24 * time.Hour)
	batch := p.Batch
	if batch <= 0 {
		batch = defaultBatch
	}
	for i := 0; i < maxBatchesPerSweep && ctx.Err() == nil; i++ {
		var n int64
		if err := p.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			var err error
			n, err = q.ExpireFaceSightings(ctx, db.ExpireFaceSightingsParams{Cutoff: cutoff, MaxRows: batch})
			return err
		}); err != nil {
			p.record(ctx, res)
			return res, err
		}
		res.Faces += n
		if n < int64(batch) {
			break
		}
	}
	p.record(ctx, res)
	return res, ctx.Err()
}

// record logs and audits what a sweep erased, including the batches that committed before a later one failed.
func (p *Purger) record(ctx context.Context, res Result) {
	if res.Faces == 0 {
		return
	}
	p.log().InfoContext(ctx, "retention erased unnamed faces", "faces", res.Faces, "days", res.Days, "pressure", res.Pressure)
	if err := p.audit(context.WithoutCancel(ctx), res); err != nil {
		p.log().WarnContext(ctx, "retention audit entry not written", "error", err)
	}
}

func (p *Purger) audit(ctx context.Context, res Result) error {
	details, err := json.Marshal(map[string]any{"class": "face_unknown", "erased": res.Faces, "days": res.Days, "pressure": res.Pressure})
	if err != nil {
		return err
	}
	return p.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			ActorName: "system", Action: ActionPurged, TargetType: "retention_settings", Details: details,
		})
	})
}

// VolumePercent returns a function that reports how full the filesystem holding path is. Unmeasurable
// (empty or missing path) is reported as ok=false and never counts as pressure.
func VolumePercent(path string) func() (float64, bool) {
	return func() (float64, bool) {
		total, free, ok := hoststat.DiskUsage(path)
		if !ok {
			return 0, false
		}
		return float64(total-min(free, total)) / float64(total) * 100, true
	}
}

// FacePreview counts what a retention of days would erase now: unnamed faces and the crop bytes they hold.
func FacePreview(ctx context.Context, s *store.Store, days int, now time.Time) (records, bytes int64, err error) {
	if days == ForeverDays {
		return 0, 0, nil
	}
	err = s.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		row, err := q.CountExpirableFaceSightings(ctx, now.Add(-time.Duration(days)*24*time.Hour))
		records, bytes = row.Records, row.Bytes
		return err
	})
	return records, bytes, err
}
