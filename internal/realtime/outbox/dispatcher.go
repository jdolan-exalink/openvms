// Package outbox dispatches durable realtime publications to JetStream.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var ErrLeaseLost = errors.New("realtime publication lease lost")

type Repository interface {
	Claim(context.Context, int, time.Duration, int) ([]db.RealtimePublicationOutbox, error)
	MarkSent(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	Retry(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Duration, int, string) error
}

type Publisher interface {
	Publish(context.Context, db.RealtimePublicationOutbox) error
}

type Config struct {
	PollInterval time.Duration
	Lease        time.Duration
	RetryBase    time.Duration
	RetryMax     time.Duration
	MaxAttempts  int
	BatchSize    int
}

type Dispatcher struct {
	Repository Repository
	Publisher  Publisher
	Config     Config
	Log        *slog.Logger
}

// Run polls until canceled. An in-flight row remains leased on shutdown and becomes
// eligible after lease expiry, preserving at-least-once delivery across crashes.
func (d *Dispatcher) Run(ctx context.Context) {
	interval := d.Config.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := d.ProcessBatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.log().ErrorContext(ctx, "claim realtime publications", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) ProcessBatch(ctx context.Context) error {
	cfg := d.Config
	rows, err := d.Repository.Claim(ctx, positive(cfg.BatchSize, 32), positiveDuration(cfg.Lease, 30*time.Second), positive(cfg.MaxAttempts, 8))
	if err != nil {
		return err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AvailableAt.Equal(rows[j].AvailableAt) {
			if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
				return rows[i].ID.String() < rows[j].ID.String()
			}
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].AvailableAt.Before(rows[j].AvailableAt)
	})
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if row.LeaseToken == nil {
			d.log().ErrorContext(ctx, "claimed realtime publication without lease token", "id", row.ID)
			continue
		}
		if err := d.Publisher.Publish(ctx, row); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			delay := retryDelay(row.Attempts, cfg.RetryBase, cfg.RetryMax)
			if row.Attempts >= int32(positive(cfg.MaxAttempts, 8)) {
				delay = 0
			}
			if retryErr := d.Repository.Retry(ctx, row.TenantID, row.ID, *row.LeaseToken, delay, positive(cfg.MaxAttempts, 8), err.Error()); retryErr != nil {
				d.log().ErrorContext(ctx, "retry realtime publication", "id", row.ID, "error", retryErr)
			}
			continue
		}
		if err := d.Repository.MarkSent(ctx, row.TenantID, row.ID, *row.LeaseToken); err != nil {
			d.log().ErrorContext(ctx, "ack realtime publication", "id", row.ID, "error", err)
		}
	}
	return nil
}

func (d *Dispatcher) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

func retryDelay(attempts int32, base, maximum time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if maximum <= 0 {
		maximum = time.Minute
	}
	if base >= maximum {
		return maximum
	}
	delay := base
	for attempt := int32(1); attempt < attempts; attempt++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func positive(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func positiveDuration(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}

type StoreRepository struct{ Store *store.Store }

func (r StoreRepository) Claim(ctx context.Context, maxRows int, lease time.Duration, maxAttempts int) ([]db.RealtimePublicationOutbox, error) {
	var claimed []db.RealtimePublicationOutbox
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		claimed, err = q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{
			LeaseSeconds: lease.Seconds(), MaxAttempts: int32(maxAttempts), MaxRows: int32(maxRows),
		})
		return err
	})
	return claimed, err
}

func (r StoreRepository) MarkSent(ctx context.Context, tenantID, id, token uuid.UUID) error {
	return r.update(ctx, tenantID, id, func(q *db.Queries) (int64, error) {
		return q.MarkRealtimePublicationSent(ctx, db.MarkRealtimePublicationSentParams{ID: id, LeaseToken: &token})
	})
}

func (r StoreRepository) Retry(ctx context.Context, tenantID, id, token uuid.UUID, delay time.Duration, maxAttempts int, message string) error {
	return r.update(ctx, tenantID, id, func(q *db.Queries) (int64, error) {
		return q.RetryRealtimePublication(ctx, db.RetryRealtimePublicationParams{
			ID: id, LeaseToken: &token, MaxAttempts: int32(maxAttempts), RetrySeconds: delay.Seconds(), LastError: &message,
		})
	})
}

func (r StoreRepository) update(ctx context.Context, tenantID, id uuid.UUID, fn func(*db.Queries) (int64, error)) error {
	var affected int64
	err := r.Store.Tx(ctx, store.TenantScope{TenantID: tenantID}, func(q *db.Queries) error {
		var err error
		affected, err = fn(q)
		return err
	})
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s", ErrLeaseLost, id)
	}
	return nil
}

type jetStream interface {
	PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type JetStreamPublisher struct{ JS jetStream }

// Publish waits for JetStream acceptance and uses the durable row ID for deduplication.
// A crash between acceptance and the database acknowledgement can still redeliver.
func (p JetStreamPublisher) Publish(ctx context.Context, row db.RealtimePublicationOutbox) error {
	message := &nats.Msg{Subject: row.Subject, Data: row.Payload, Header: nats.Header{nats.MsgIdHdr: []string{row.ID.String()}}}
	_, err := p.JS.PublishMsg(ctx, message)
	return err
}
