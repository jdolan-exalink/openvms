package media

import (
	"context"
	"io"
	"sync"
	"time"
)

// BandwidthLimiter manages global and per-server token-bucket rate limiting for low-priority export traffic.
type BandwidthLimiter struct {
	mu           sync.Mutex
	globalRate   int64 // bytes per second (0 = unlimited)
	serverRate   int64 // default per-server bytes per second (0 = unlimited)
	globalTokens float64
	serverTokens map[string]float64
	lastUpdate   time.Time
}

// NewBandwidthLimiter initializes a token bucket limiter.
func NewBandwidthLimiter(globalBytesPerSec, defaultServerBytesPerSec int64) *BandwidthLimiter {
	now := time.Now()
	return &BandwidthLimiter{
		globalRate:   globalBytesPerSec,
		serverRate:   defaultServerBytesPerSec,
		globalTokens: float64(globalBytesPerSec),
		serverTokens: make(map[string]float64),
		lastUpdate:   now,
	}
}

// Acquire waits until n bytes can be transferred according to rate limits.
func (l *BandwidthLimiter) Acquire(ctx context.Context, serverID string, n int) error {
	if n <= 0 {
		return nil
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.lastUpdate).Seconds()
		l.lastUpdate = now

		// Replenish global tokens
		if l.globalRate > 0 {
			l.globalTokens += elapsed * float64(l.globalRate)
			if l.globalTokens > float64(l.globalRate) {
				l.globalTokens = float64(l.globalRate)
			}
		}

		// Replenish server tokens
		if l.serverRate > 0 {
			tokens, ok := l.serverTokens[serverID]
			if !ok {
				tokens = float64(l.serverRate)
			} else {
				tokens += elapsed * float64(l.serverRate)
			}
			if tokens > float64(l.serverRate) {
				tokens = float64(l.serverRate)
			}
			l.serverTokens[serverID] = tokens
		}

		needed := float64(n)
		hasGlobal := l.globalRate <= 0 || l.globalTokens >= needed
		hasServer := l.serverRate <= 0 || l.serverTokens[serverID] >= needed

		if hasGlobal && hasServer {
			if l.globalRate > 0 {
				l.globalTokens -= needed
			}
			if l.serverRate > 0 {
				l.serverTokens[serverID] -= needed
			}
			l.mu.Unlock()
			return nil
		}

		// Calculate sleep time needed to replenish tokens
		var waitGlobal, waitServer float64
		if l.globalRate > 0 && l.globalTokens < needed {
			waitGlobal = (needed - l.globalTokens) / float64(l.globalRate)
		}
		if l.serverRate > 0 && l.serverTokens[serverID] < needed {
			waitServer = (needed - l.serverTokens[serverID]) / float64(l.serverRate)
		}

		waitSec := waitGlobal
		if waitServer > waitSec {
			waitSec = waitServer
		}
		if waitSec < 0.005 {
			waitSec = 0.005
		}
		if waitSec > 0.5 {
			waitSec = 0.5
		}
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(waitSec * float64(time.Second))):
		}
	}
}

// ThrottledReader limits read throughput via BandwidthLimiter and calls onBytes for progress tracking.
type ThrottledReader struct {
	r        io.Reader
	ctx      context.Context
	limiter  *BandwidthLimiter
	serverID string
	onBytes  func(n int)
}

func NewThrottledReader(ctx context.Context, r io.Reader, limiter *BandwidthLimiter, serverID string, onBytes func(n int)) *ThrottledReader {
	return &ThrottledReader{
		r:        r,
		ctx:      ctx,
		limiter:  limiter,
		serverID: serverID,
		onBytes:  onBytes,
	}
}

func (tr *ThrottledReader) Read(p []byte) (int, error) {
	if tr.ctx.Err() != nil {
		return 0, tr.ctx.Err()
	}
	// Limit chunk size to keep burst small
	chunk := len(p)
	if chunk > 64*1024 {
		chunk = 64 * 1024
	}
	n, err := tr.r.Read(p[:chunk])
	if n > 0 {
		if tr.limiter != nil {
			if aerr := tr.limiter.Acquire(tr.ctx, tr.serverID, n); aerr != nil {
				return n, aerr
			}
		}
		if tr.onBytes != nil {
			tr.onBytes(n)
		}
	}
	return n, err
}
