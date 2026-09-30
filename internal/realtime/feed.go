package realtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Source consumes new messages of a stream, filtered to subjects, until ctx ends or the
// consumer fails. It is the seam between Feed and JetStream.
type Source interface {
	Consume(ctx context.Context, stream string, subjects []string, handle func(seq uint64, subject string, data []byte)) error
}

// Feed pumps JetStream messages into the Hub: one consumer per distinct stream of Routes,
// restarted with a delay when it fails, until the context passed to Run ends.
type Feed struct {
	Source Source
	Hub    *Hub
	Routes []Route
	Log    *slog.Logger
	// RetryDelay is the initial pause before a failed consumer is recreated. Zero means 2s.
	// Consecutive failures double it up to MaxRetryDelay.
	RetryDelay time.Duration
	// MaxRetryDelay caps the backoff. Zero means 30s.
	MaxRetryDelay time.Duration
	// HealthyAfter is how long a consumer must run before its failure counts as a fresh one
	// and the backoff resets to RetryDelay. Zero means 60s.
	HealthyAfter time.Duration
}

// nextRetryDelay returns the pause to use after the consumer that just ended: doubled up to
// maxDelay, or reset to initial when that consumer had run for at least healthy.
func nextRetryDelay(cur, initial, maxDelay, ranFor, healthy time.Duration) time.Duration {
	if ranFor >= healthy {
		return initial
	}
	if next := cur * 2; next < maxDelay {
		return next
	}
	return maxDelay
}

// Run blocks until ctx is cancelled. Consumer failures are logged and retried, never fatal.
func (f *Feed) Run(ctx context.Context) {
	log := f.Log
	if log == nil {
		log = slog.Default()
	}
	initial, maxDelay, healthy := f.RetryDelay, f.MaxRetryDelay, f.HealthyAfter
	if initial <= 0 {
		initial = 2 * time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	if maxDelay < initial {
		maxDelay = initial
	}
	if healthy <= 0 {
		healthy = time.Minute
	}
	var wg sync.WaitGroup
	for stream, subjects := range subjectsByStream(f.Routes) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delay := initial
			for ctx.Err() == nil {
				began := time.Now()
				err := f.Source.Consume(ctx, stream, subjects, func(seq uint64, subject string, data []byte) {
					f.Hub.DispatchStream(stream, seq, subject, data)
				})
				if ctx.Err() != nil {
					return
				}
				if time.Since(began) >= healthy {
					delay = initial // it was healthy for a while: this is a fresh failure
				}
				log.Warn("realtime: consumer stopped, restarting", "stream", stream, "error", err, "retry_in", delay)
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				delay = nextRetryDelay(delay, initial, maxDelay, 0, healthy)
			}
		}()
	}
	wg.Wait()
}

// subjectsByStream groups route subjects per stream, without duplicates, in route order.
func subjectsByStream(routes []Route) map[string][]string {
	out := map[string][]string{}
	for _, r := range routes {
		dup := false
		for _, s := range out[r.Stream] {
			dup = dup || s == r.Subject
		}
		if !dup {
			out[r.Stream] = append(out[r.Stream], r.Subject)
		}
	}
	return out
}

// JetStreamSource is the production Source: an ephemeral ordered consumer that delivers only
// messages published after it was created (a restart never replays history; clients refetch
// through the REST API).
type JetStreamSource struct {
	JS jetstream.JetStream
}

func (s JetStreamSource) Consume(ctx context.Context, stream string, subjects []string, handle func(uint64, string, []byte)) error {
	cons, err := s.JS.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: subjects,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return fmt.Errorf("ordered consumer on %s: %w", stream, err)
	}
	failed := make(chan error, 1)
	cc, err := cons.Consume(
		func(m jetstream.Msg) {
			var seq uint64
			if meta, err := m.Metadata(); err == nil && meta != nil {
				seq = meta.Sequence.Stream
			}
			handle(seq, m.Subject(), m.Data())
		},
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			// Ordered consumers recreate themselves on gaps; these terminal errors are reported
			// at once. Any other way the consume can stop is caught by awaitConsume via Closed().
			if errors.Is(err, jetstream.ErrConsumerDeleted) || errors.Is(err, jetstream.ErrNoHeartbeat) {
				select {
				case failed <- err:
				default:
				}
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("consume %s: %w", stream, err)
	}
	defer cc.Stop()
	return awaitConsume(ctx, cc, failed)
}

// errConsumeStopped reports a consume context that ended without ctx being cancelled.
var errConsumeStopped = errors.New("consume context stopped")

// awaitConsume blocks until ctx ends, a terminal error arrives, or the consume context stops
// for any reason (connection closed, drain, ordered-consumer reset failure): a silent stop
// would otherwise leave the feed stalled with no log and no restart.
func awaitConsume(ctx context.Context, cc interface{ Closed() <-chan struct{} }, failed <-chan error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-failed:
		return err
	case <-cc.Closed():
		return errConsumeStopped
	}
}
