package alarms

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	defaultSweepInterval       = time.Minute
	defaultSweepBatch    int32 = 500
)

// AutoCloser closes alarms nobody works on: open or acknowledged alarms whose last event is older
// than alarm_settings.auto_close_after. assigned and investigating alarms are deliberately left
// alone: a person owns them and decides when they end.
type AutoCloser struct {
	Store *store.Store
	Pub   Publisher
	Log   *slog.Logger
	// Interval between sweeps (default 1 minute).
	Interval time.Duration
	// Batch bounds the alarms closed per sweep so a large backlog is drained over several ticks
	// instead of in one long transaction (default 500).
	Batch int32
	// Now is the clock; tests inject a fake one.
	Now func() time.Time
}

func (a *AutoCloser) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Run sweeps every Interval until ctx is done.
func (a *AutoCloser) Run(ctx context.Context) {
	interval := a.Interval
	if interval <= 0 {
		interval = defaultSweepInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := a.Sweep(ctx); err != nil {
				a.Log.WarnContext(ctx, "alarm auto-close sweep failed", "error", err)
			} else if n > 0 {
				a.Log.InfoContext(ctx, "alarms auto-closed", "count", n)
			}
		}
	}
}

// Sweep closes one batch of inactive alarms and returns how many it closed. Realtime updates are
// published after the transaction commits.
func (a *AutoCloser) Sweep(ctx context.Context) (int, error) {
	batch := a.Batch
	if batch <= 0 {
		batch = defaultSweepBatch
	}
	now := a.now()
	var closed []ClosedAlarm
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		q := db.New(tx)
		settings, err := q.GetAlarmSettings(ctx)
		if err != nil {
			return store.Classify(err)
		}
		window := time.Duration(settings.AutoCloseSeconds * float64(time.Second))
		ids, err := q.ListInactiveAlarms(ctx, db.ListInactiveAlarmsParams{Cutoff: now.Add(-window), LimitCount: batch})
		if err != nil {
			return store.Classify(err)
		}
		for _, id := range ids {
			c, ok, err := autoClose(ctx, q, id, now, window)
			if err != nil {
				return err
			}
			if ok {
				closed = append(closed, c)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, c := range closed {
		PublishAlarm(ctx, a.Pub, a.Log, "closed", "closed", c.TenantID, c.SiteID, c.CameraID, c.EventID, c.ID, c.UpdatedAt)
	}
	return len(closed), nil
}
