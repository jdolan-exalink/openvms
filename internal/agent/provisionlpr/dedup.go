package provisionlpr

import (
	"sync"
	"time"
)

// Deduplicator suppresses duplicate plate reads within a sliding window.
type Deduplicator struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]time.Time
}

// NewDeduplicator creates a Deduplicator with the given window (bounded between 5s and 120s).
func NewDeduplicator(window time.Duration) *Deduplicator {
	if window < 5*time.Second {
		window = 5 * time.Second
	} else if window > 120*time.Second {
		window = 120 * time.Second
	}
	return &Deduplicator{
		window: window,
		seen:   make(map[string]time.Time),
	}
}

// IsDuplicate returns true if the same plate on the same camera was received within the window.
// If it is not a duplicate, the record is updated with now and returns false.
func (d *Deduplicator) IsDuplicate(cameraID, plate string, now time.Time) bool {
	if plate == "" {
		return false
	}
	key := cameraID + ":" + plate

	d.mu.Lock()
	defer d.mu.Unlock()

	d.cleanupLocked(now)

	if lastTime, exists := d.seen[key]; exists {
		if now.Sub(lastTime) < d.window {
			return true
		}
	}

	d.seen[key] = now
	return false
}

func (d *Deduplicator) cleanupLocked(now time.Time) {
	for k, t := range d.seen {
		if now.Sub(t) >= d.window {
			delete(d.seen, k)
		}
	}
}
