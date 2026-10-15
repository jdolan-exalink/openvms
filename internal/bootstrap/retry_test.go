package bootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/bootstrap"
)

type fakeSleeper struct{ waits []time.Duration }

func (f *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	return nil
}

func retrier(f *fakeSleeper) bootstrap.Retrier {
	return bootstrap.Retrier{Attempts: 5, Base: time.Second, Sleep: f.sleep}
}

func TestRetrierSucceedsAfterTransientFailures(t *testing.T) {
	f := &fakeSleeper{}
	var buf bytes.Buffer
	calls := 0
	err := retrier(f).Do(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)), "initial admin", func() error {
		calls++
		if calls < 3 {
			return errors.New("db not ready")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if len(f.waits) != 2 || f.waits[0] != time.Second || f.waits[1] != 2*time.Second {
		t.Fatalf("waits = %v, want 1s then 2s", f.waits)
	}
	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("unexpected ERROR: %q", buf.String())
	}
}

func TestRetrierGivesUpWithClearError(t *testing.T) {
	f := &fakeSleeper{}
	var buf bytes.Buffer
	calls := 0
	err := retrier(f).Do(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)), "initial admin", func() error {
		calls++
		return errors.New("boom")
	})
	if err == nil || calls != 5 || len(f.waits) != 4 || f.waits[3] != 8*time.Second {
		t.Fatalf("err=%v calls=%d waits=%v", err, calls, f.waits)
	}
	out := buf.String()
	if strings.Count(out, "level=ERROR") != 1 || !strings.Contains(out, "initial admin") || !strings.Contains(out, "vmsctl bootstrap") {
		t.Fatalf("log = %q", out)
	}
}

func TestRetrierStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r := bootstrap.Retrier{Attempts: 5, Base: time.Second, Sleep: func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}}
	err := r.Do(ctx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "x", func() error {
		calls++
		return errors.New("boom")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
