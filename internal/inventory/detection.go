package inventory

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// detectionIdleFPS is the detection_fps at or below which a camera is not being analysed.
const detectionIdleFPS = 0.05

// DefaultDetectionStallHold is how long every camera that delivers video must report no
// detections before the server is declared stalled. detection_fps is also 0 in a quiet scene,
// so the hold is long enough to ride out a night without activity at a busy site.
const DefaultDetectionStallHold = 30 * time.Minute

// DetectionState is the verdict of one observation of a server's detection activity.
type DetectionState struct {
	Stalled bool
	// Since is when detection was first seen idle; zero when it is not idle.
	Since time.Time
}

// DetectionTracker turns the per-camera stats of successive Frigate polls into a hold/recover
// state machine per server: Frigate keeps recording and streaming while its detection pipeline
// is wedged, which camera_fps alone never shows. State is in memory.
type DetectionTracker struct {
	hold time.Duration

	mu        sync.Mutex
	idleSince map[uuid.UUID]time.Time
}

func NewDetectionTracker(hold time.Duration) *DetectionTracker {
	return &DetectionTracker{hold: hold, idleSince: map[uuid.UUID]time.Time{}}
}

// Observe records one stats reading of a server at now. Only cameras that deliver video count:
// one without signal cannot detect and says nothing about the pipeline.
func (t *DetectionTracker) Observe(server uuid.UUID, cameras []frigate.CameraStats, now time.Time) DetectionState {
	t.mu.Lock()
	defer t.mu.Unlock()
	live := 0
	for _, c := range cameras {
		if c.CameraFPS <= noSignalFPS {
			continue
		}
		if c.DetectionFPS > detectionIdleFPS {
			delete(t.idleSince, server)
			return DetectionState{}
		}
		live++
	}
	if live == 0 {
		delete(t.idleSince, server)
		return DetectionState{}
	}
	since, ok := t.idleSince[server]
	if !ok {
		since = now
		t.idleSince[server] = now
	}
	return DetectionState{Stalled: now.Sub(since) >= t.hold, Since: since}
}

// Forget drops a server's hold, e.g. when it is unreachable and its stats mean nothing.
func (t *DetectionTracker) Forget(server uuid.UUID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.idleSince, server)
}
