package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// EnqueueInput describes one notification to fan out to the selected channels.
type EnqueueInput struct {
	TenantID       uuid.UUID
	RuleID         *uuid.UUID
	NotificationID *uuid.UUID
	ChannelIDs     []uuid.UUID
	Title          string
	Body           string
	Link           string
	Severity       string
	OccurredAt     time.Time
}

// storedPayload is what a delivery row keeps of the message.
type storedPayload struct {
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Link       string    `json:"link,omitempty"`
	Severity   string    `json:"severity"`
	RuleID     string    `json:"rule_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Enqueue inserts one pending delivery per destination of every enabled selected channel, inside
// the caller's transaction so rule firings and their deliveries commit together. The inserts run
// under a savepoint: a failure here is rolled back and returned without poisoning the caller's
// transaction, so rule evaluation is never blocked by the outbox.
func Enqueue(ctx context.Context, tx pgx.Tx, in EnqueueInput) (n int, err error) {
	if len(in.ChannelIDs) == 0 {
		return 0, nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = sp.Rollback(ctx)
			n = 0
			return
		}
		err = sp.Commit(ctx)
	}()
	q := db.New(sp)
	channels, err := q.ListEnabledNotificationChannelsByIDs(ctx, db.ListEnabledNotificationChannelsByIDsParams{TenantID: in.TenantID, Ids: in.ChannelIDs})
	if err != nil {
		return 0, err
	}
	p := storedPayload{Title: in.Title, Body: in.Body, Link: in.Link, Severity: in.Severity, OccurredAt: in.OccurredAt}
	if p.OccurredAt.IsZero() {
		p.OccurredAt = time.Now()
	}
	if in.RuleID != nil {
		p.RuleID = in.RuleID.String()
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	for _, ch := range channels {
		for _, dest := range Destinations(Type(ch.Type), decodeConfig(ch)) {
			cid := ch.ID
			if err = q.InsertNotificationDelivery(ctx, db.InsertNotificationDeliveryParams{
				TenantID: in.TenantID, ChannelID: &cid, ChannelName: ch.Name, ChannelType: ch.Type,
				RuleID: in.RuleID, NotificationID: in.NotificationID, Destination: dest, Payload: payload,
			}); err != nil {
				return 0, err
			}
			n++
		}
	}
	return n, nil
}

// EnqueueLogged is Enqueue for callers that only want failures logged.
func EnqueueLogged(ctx context.Context, tx pgx.Tx, log *slog.Logger, in EnqueueInput) {
	if _, err := Enqueue(ctx, tx, in); err != nil {
		log.WarnContext(ctx, "enqueue channel deliveries", "error", err, "rule_id", in.RuleID)
	}
}

// Worker sends outbox rows. Rows are leased when claimed (next_attempt_at is pushed out and
// attempts bumped), then either marked sent, rescheduled with exponential backoff, or marked
// failed once attempts reach MaxAttempts or the error is permanent. Several workers can run
// side by side: claiming uses FOR UPDATE SKIP LOCKED.
type Worker struct {
	Store  *store.Store
	Sealer *secrets.Sealer
	Log    *slog.Logger
	Deps   Deps
	// Interval between polls (default 5s). Batch rows are claimed per poll (default 10).
	Interval time.Duration
	Batch    int
	// MaxAttempts per row (default 5); the delay after attempt n is BackoffBase*2^(n-1)
	// capped at BackoffMax (defaults 30s and 30m).
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// Lease is how long a claimed row stays invisible to other workers (default 2m).
	Lease time.Duration
}

const (
	defaultInterval    = 5 * time.Second
	defaultBatch       = 10
	defaultMaxAttempts = 5
	defaultBackoffBase = 30 * time.Second
	defaultBackoffMax  = 30 * time.Minute
	defaultLease       = 2 * time.Minute
	sendConcurrency    = 4
	maxStoredError     = 500
)

func orDefault[T ~int | ~int64](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

func (w *Worker) log() *slog.Logger {
	if w.Log == nil {
		return slog.Default()
	}
	return w.Log
}

func (w *Worker) deps() Deps {
	d := w.Deps
	if d.HTTP == nil {
		d.HTTP = NewHTTPClient(sendTimeout)
	}
	return d
}

func (w *Worker) backoff(attempts int) time.Duration {
	base, maxDelay := orDefault(w.BackoffBase, defaultBackoffBase), orDefault(w.BackoffMax, defaultBackoffMax)
	d := base
	for i := 1; i < attempts && d < maxDelay; i++ {
		d *= 2
	}
	return min(d, maxDelay)
}

// Run polls until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(orDefault(w.Interval, defaultInterval))
	defer t.Stop()
	for {
		if n, err := w.RunOnce(ctx); err != nil {
			w.log().ErrorContext(ctx, "notification delivery poll", "error", err)
		} else if n > 0 {
			continue // more rows may be due: drain before sleeping
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce claims and processes one batch and returns how many rows it handled.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	var rows []db.NotificationDelivery
	err := w.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		rows, err = q.ClaimDueNotificationDeliveries(ctx, db.ClaimDueNotificationDeliveriesParams{
			LeaseSeconds: orDefault(w.Lease, defaultLease).Seconds(),
			MaxRows:      int32(orDefault(w.Batch, defaultBatch)), //nolint:gosec // small bounded value
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, sendConcurrency)
	for _, row := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			w.process(ctx, row)
		}()
	}
	wg.Wait()
	return len(rows), nil
}

func (w *Worker) process(ctx context.Context, row db.NotificationDelivery) {
	sendErr := w.send(ctx, row)
	err := w.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		switch {
		case sendErr == nil:
			return q.MarkNotificationDeliverySent(ctx, row.ID)
		case IsPermanent(sendErr) || int(row.Attempts) >= orDefault(w.MaxAttempts, defaultMaxAttempts):
			return q.MarkNotificationDeliveryFailed(ctx, db.MarkNotificationDeliveryFailedParams{ID: row.ID, LastError: errText(sendErr)})
		default:
			return q.MarkNotificationDeliveryRetry(ctx, db.MarkNotificationDeliveryRetryParams{
				ID: row.ID, LastError: errText(sendErr), DelaySeconds: w.backoff(int(row.Attempts)).Seconds(),
			})
		}
	})
	if err != nil {
		// The lease expires and the row is retried; a send may repeat, delivery stays at-least-once.
		w.log().ErrorContext(ctx, "record delivery outcome", "error", err, "delivery_id", row.ID)
	}
	if sendErr != nil {
		w.log().WarnContext(ctx, "notification delivery failed", "delivery_id", row.ID, "channel_id", row.ChannelID, "attempt", row.Attempts, "error", sendErr)
	}
}

func errText(err error) *string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > maxStoredError {
		s = s[:maxStoredError]
	}
	return &s
}

// send resolves the channel and delivers the row. Errors that retrying cannot fix are permanent.
func (w *Worker) send(ctx context.Context, row db.NotificationDelivery) error {
	if row.ChannelID == nil {
		return permanent(errors.New("channel was deleted"))
	}
	var ch db.NotificationChannel
	err := w.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		ch, err = q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: *row.ChannelID, TenantID: row.TenantID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return permanent(errors.New("channel was deleted"))
	}
	if err != nil {
		return err
	}
	if !ch.Enabled {
		return permanent(errors.New("channel is disabled"))
	}
	sec, err := OpenSecrets(w.Sealer, ch)
	if err != nil {
		return permanent(err)
	}
	sender, err := NewSender(Type(ch.Type), decodeConfig(ch), sec, w.deps())
	if err != nil {
		return permanent(err)
	}
	var p storedPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return permanent(fmt.Errorf("decode payload: %w", err))
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout+5*time.Second)
	defer cancel()
	return sender.Send(sctx, Message{
		ID: row.ID.String(), Title: p.Title, Body: p.Body, Link: p.Link, Severity: p.Severity,
		RuleID: p.RuleID, OccurredAt: p.OccurredAt,
	}, row.Destination)
}
