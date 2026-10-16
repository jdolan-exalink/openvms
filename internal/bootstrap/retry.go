package bootstrap

import (
	"context"
	"log/slog"
	"time"
)

// Retrier runs startup seeding with bounded exponential backoff, so a database that is not
// quite ready does not leave the install without its admin or tenant.
type Retrier struct {
	// Attempts is the total number of tries; Base the wait after the first failure, doubling.
	Attempts int
	Base     time.Duration
	// Sleep waits d or returns the context error; injectable for tests.
	Sleep func(ctx context.Context, d time.Duration) error
}

// DefaultRetrier makes 5 attempts, waiting 1s, 2s, 4s and 8s between them.
func DefaultRetrier() Retrier {
	return Retrier{Attempts: 5, Base: time.Second, Sleep: sleepContext}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Do runs fn until it succeeds, attempts run out or ctx ends. After the last failed attempt
// it logs one ERROR naming what may be missing and how to recover, and returns the error.
func (r Retrier) Do(ctx context.Context, log *slog.Logger, what string, fn func() error) error {
	sleep := r.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	wait := r.Base
	var err error
	for attempt := 1; attempt <= r.Attempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt == r.Attempts {
			break
		}
		log.Warn("startup seeding failed; retrying", "what", what, "attempt", attempt, "error", err, "retry_in", wait)
		if serr := sleep(ctx, wait); serr != nil {
			return serr
		}
		wait *= 2
	}
	log.Error("startup seeding gave up; "+what+" may be missing. Restart the api or run `vmsctl bootstrap`",
		"what", what, "attempts", r.Attempts, "error", err)
	return err
}
