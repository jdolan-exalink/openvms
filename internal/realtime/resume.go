package realtime

import (
	"sync"
	"time"
)

const (
	defaultRingMaxAge = 5 * time.Minute
	defaultRingCap    = 50000
)

type ringItem struct {
	msg Message
	at  time.Time
}

// RingBuffer stores decoded messages per stream for up to 5 minutes, capped at 50,000 per stream.
type RingBuffer struct {
	mu      sync.RWMutex
	maxAge  time.Duration
	maxCap  int
	streams map[string][]ringItem
	now     func() time.Time
}

// NewRingBuffer creates a RingBuffer with standard 5-minute / 50k limits.
func NewRingBuffer() *RingBuffer {
	return &RingBuffer{
		maxAge:  defaultRingMaxAge,
		maxCap:  defaultRingCap,
		streams: make(map[string][]ringItem),
		now:     time.Now,
	}
}

// SetClock allows replacing the time source in unit tests.
func (r *RingBuffer) SetClock(now func() time.Time) {
	r.mu.Lock()
	r.now = now
	r.mu.Unlock()
}

// Add appends a message to the stream's ring buffer and evicts expired or excess entries.
func (r *RingBuffer) Add(m Message) {
	if m.Stream == "" || m.Seq == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	items := r.streams[m.Stream]
	items = append(items, ringItem{msg: m, at: now})

	// Evict entries older than maxAge
	cutoff := now.Add(-r.maxAge)
	startIdx := 0
	for startIdx < len(items) && items[startIdx].at.Before(cutoff) {
		startIdx++
	}
	if startIdx > 0 {
		items = items[startIdx:]
	}

	// Cap to maxCap
	if len(items) > r.maxCap {
		items = items[len(items)-r.maxCap:]
	}

	r.streams[m.Stream] = items
}

// Replay finds all messages strictly after lastSeq for stream.
// If lastSeq has fallen behind the oldest entry in the ring buffer, it returns nil, false (gap outside buffer).
// Otherwise, it returns the missed messages and true.
func (r *RingBuffer) Replay(stream string, lastSeq uint64) ([]Message, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items, ok := r.streams[stream]
	if !ok || len(items) == 0 {
		return nil, true
	}

	oldestSeq := items[0].msg.Seq
	newestSeq := items[len(items)-1].msg.Seq

	if lastSeq >= newestSeq {
		return nil, true
	}

	// If lastSeq + 1 is less than oldestSeq, a gap fell out of the buffer
	if lastSeq+1 < oldestSeq {
		return nil, false
	}

	var out []Message
	for _, it := range items {
		if it.msg.Seq > lastSeq {
			out = append(out, it.msg)
		}
	}
	return out, true
}
