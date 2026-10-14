package inventory

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// noSignalFPS is the camera_fps at or below which Frigate is considered to receive no
// video from the camera (Frigate reports a rolling average, so a dead input decays to 0).
const noSignalFPS = 0.05

// DefaultNoSignalHold is how long camera_fps must stay at ~0 before the camera is declared
// without signal. The health poller runs every 30 s (HEALTH_INTERVAL), so 60 s is two
// consecutive polls: one zero reading is a glitch, two is an unplugged camera.
const DefaultNoSignalHold = 60 * time.Second

// SignalState is the verdict of one observation of a camera's input signal.
type SignalState int

const (
	// SignalUnknown: Frigate did not report the camera, so nothing is claimed.
	SignalUnknown SignalState = iota
	// SignalOK: the camera delivers frames to Frigate.
	SignalOK
	// SignalHolding: camera_fps is ~0 but the hold period has not elapsed yet.
	SignalHolding
	// SignalLost: camera_fps stayed ~0 for the whole hold period.
	SignalLost
)

// SignalTracker turns the per-camera camera_fps of successive Frigate stats polls into a
// hold/recover state machine. State is in memory; the health poller owns the instance.
type SignalTracker struct {
	hold time.Duration

	mu        sync.Mutex
	zeroSince map[uuid.UUID]time.Time
}

func NewSignalTracker(hold time.Duration) *SignalTracker {
	return &SignalTracker{hold: hold, zeroSince: map[uuid.UUID]time.Time{}}
}

// Observe records one stats reading at now. reported is false when Frigate did not list
// the camera in /api/stats.
func (t *SignalTracker) Observe(id uuid.UUID, fps float64, reported bool, now time.Time) SignalState {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !reported {
		delete(t.zeroSince, id)
		return SignalUnknown
	}
	if fps > noSignalFPS {
		delete(t.zeroSince, id)
		return SignalOK
	}
	since, ok := t.zeroSince[id]
	if !ok {
		since = now
		t.zeroSince[id] = now
	}
	if now.Sub(since) >= t.hold {
		return SignalLost
	}
	return SignalHolding
}

// NoSignal reports whether a persisted camera row means "Frigate sees the camera but gets
// no video from it": the poller marks a camera offline with a reported fps of ~0 only after
// the hold elapsed (see HealthPoller). A camera Frigate did not report has a nil fps and an
// unknown or unreachable server leaves status "unknown", so neither counts.
func NoSignal(status string, fps *float32) bool {
	return status == "offline" && fps != nil && float64(*fps) <= noSignalFPS
}
