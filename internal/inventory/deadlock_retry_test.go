package inventory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryOnDeadlockRunsAgainOnlyForADeadlock(t *testing.T) {
	deadlock := fmt.Errorf("delete server cameras: %w", &pgconn.PgError{Code: "40P01"})
	tests := []struct {
		name      string
		failures  []error
		wantCalls int
		wantErr   bool
	}{
		{name: "success is not retried", failures: []error{nil}, wantCalls: 1},
		{name: "a deadlock is retried until it passes", failures: []error{deadlock, deadlock, nil}, wantCalls: 3},
		{name: "it gives up after three attempts", failures: []error{deadlock, deadlock, deadlock, nil}, wantCalls: 3, wantErr: true},
		{name: "any other error is returned at once", failures: []error{errors.New("boom"), nil}, wantCalls: 1, wantErr: true},
		{name: "a foreign key violation is not retried", failures: []error{&pgconn.PgError{Code: "23503"}, nil}, wantCalls: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := retryOnDeadlock(context.Background(), func() error {
				err := tt.failures[calls]
				calls++
				return err
			})
			if calls != tt.wantCalls || (err != nil) != tt.wantErr {
				t.Fatalf("calls = %d, err = %v; want %d calls, error=%v", calls, err, tt.wantCalls, tt.wantErr)
			}
		})
	}
}

func TestRetryOnDeadlockStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryOnDeadlock(ctx, func() error {
		calls++
		cancel()
		return &pgconn.PgError{Code: "40P01"}
	})
	if calls != 1 || err == nil {
		t.Fatalf("calls = %d, err = %v; a cancelled context must not retry", calls, err)
	}
}
