package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	deadlockAttempts = 3
	deadlockBackoff  = 150 * time.Millisecond
)

// retryOnDeadlock runs fn again when Postgres aborted it as a deadlock victim (40P01), which
// happens when a long delete meets the health poller touching the same rows. Any other error, or
// a done context, is returned as is.
func retryOnDeadlock(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 1; attempt <= deadlockAttempts; attempt++ {
		if err = fn(); !isDeadlock(err) || attempt == deadlockAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(deadlockBackoff * time.Duration(attempt)):
		}
	}
	return err
}

func isDeadlock(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "40P01"
}
