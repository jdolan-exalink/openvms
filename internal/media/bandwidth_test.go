package media

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestBandwidthLimiter_Unlimited(t *testing.T) {
	limiter := NewBandwidthLimiter(0, 0)
	ctx := context.Background()

	start := time.Now()
	err := limiter.Acquire(ctx, "server-1", 10*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("unlimited acquisition took too long: %v", time.Since(start))
	}
}

func TestBandwidthLimiter_ContextCancellation(t *testing.T) {
	// 100 bytes/sec limit
	limiter := NewBandwidthLimiter(100, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Drain tokens
	_ = limiter.Acquire(context.Background(), "server-1", 100)

	// Now try to acquire 10000 bytes with cancelled context
	err := limiter.Acquire(ctx, "server-1", 10000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestThrottledReader(t *testing.T) {
	data := []byte("hello world video stream data bytes test")
	limiter := NewBandwidthLimiter(0, 0)

	bytesCounted := 0
	tr := NewThrottledReader(context.Background(), bytes.NewReader(data), limiter, "server-1", func(n int) {
		bytesCounted += n
	})

	out, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("expected %q, got %q", string(data), string(out))
	}
	if bytesCounted != len(data) {
		t.Fatalf("expected %d counted bytes, got %d", len(data), bytesCounted)
	}
}
