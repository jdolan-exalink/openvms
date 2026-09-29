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
	Consume(ctx context.Context, stream string, subjects []string, handle func(subject string, data []byte)) error
}

// Feed pumps JetStream messages into the Hub: one consumer per distinct stream of Routes,
// restarted with a delay when it fails, until the context passed to Run ends.
type Feed struct {
	Source Source
	Hub    *Hub
	Routes []Route
	Log    *slog.Logger
	// RetryDelay is the pause before a failed consumer is recreated. Zero means 2s.
	RetryDelay time.Duration
}

// Run blocks until ctx is cancelled. Consumer failures are logged and retried, never fatal.
func (f *Feed) Run(ctx context.Context) {
	log := f.Log
	if log == nil {
		log = slog.Default()
	}
	delay := f.RetryDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	var wg sync.WaitGroup
	for stream, subjects := range subjectsByStream(f.Routes) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				err := f.Source.Consume(ctx, stream, subjects, f.Hub.Dispatch)
				if ctx.Err() != nil {
					return
				}
				log.Warn("realtime: consumer stopped, restarting", "stream", stream, "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
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

func (s JetStreamSource) Consume(ctx context.Context, stream string, subjects []string, handle func(string, []byte)) error {
	cons, err := s.JS.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: subjects,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return fmt.Errorf("ordered consumer on %s: %w", stream, err)
	}
	failed := make(chan error, 1)
	cc, err := cons.Consume(
		func(m jetstream.Msg) { handle(m.Subject(), m.Data()) },
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			// Ordered consumers recreate themselves on gaps; only a terminal error ends the
			// consume, and it is reported so Feed can rebuild the consumer.
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-failed:
		return err
	}
}
