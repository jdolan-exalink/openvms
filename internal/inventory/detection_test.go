package inventory

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

func cams(pairs ...[2]float64) []frigate.CameraStats {
	out := make([]frigate.CameraStats, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, frigate.CameraStats{CameraFPS: p[0], DetectionFPS: p[1]})
	}
	return out
}

func TestDetectionTrackerStallsAfterHold(t *testing.T) {
	id := uuid.New()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := NewDetectionTracker(30 * time.Minute)
	idle := cams([2]float64{5, 0}, [2]float64{5, 0})

	if st := tr.Observe(id, idle, now); st.Stalled {
		t.Fatal("first idle reading must only start the hold")
	}
	if st := tr.Observe(id, idle, now.Add(29*time.Minute)); st.Stalled {
		t.Fatal("29 min of a 30 min hold is not a stall yet")
	}
	st := tr.Observe(id, idle, now.Add(30*time.Minute))
	if !st.Stalled || !st.Since.Equal(now) {
		t.Fatalf("after the hold: got %+v, want stalled since %v", st, now)
	}
}

func TestDetectionTrackerAnyDetectionRecovers(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	tr := NewDetectionTracker(time.Minute)
	tr.Observe(id, cams([2]float64{5, 0}, [2]float64{5, 0}), now)
	if st := tr.Observe(id, cams([2]float64{5, 0}, [2]float64{5, 1.2}), now.Add(time.Hour)); st.Stalled {
		t.Fatal("one camera detecting means Frigate is detecting")
	}
	// A new idle period restarts the hold from scratch.
	if st := tr.Observe(id, cams([2]float64{5, 0}), now.Add(time.Hour+time.Second)); st.Stalled {
		t.Fatal("hold must restart after recovery")
	}
}

func TestDetectionTrackerIgnoresCamerasWithoutVideo(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	tr := NewDetectionTracker(0)
	// A camera with no video cannot detect, so it must neither stall nor block the verdict.
	if st := tr.Observe(id, cams([2]float64{0, 0}), now); st.Stalled {
		t.Fatal("no camera delivers video: nothing to claim")
	}
	if st := tr.Observe(id, nil, now); st.Stalled {
		t.Fatal("no cameras: nothing to claim")
	}
	if st := tr.Observe(id, cams([2]float64{0, 0}, [2]float64{5, 0}), now); !st.Stalled {
		t.Fatal("the only camera with video is not detecting")
	}
}

func TestDetectionTrackerForget(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	tr := NewDetectionTracker(time.Minute)
	tr.Observe(id, cams([2]float64{5, 0}), now)
	tr.Forget(id)
	if st := tr.Observe(id, cams([2]float64{5, 0}), now.Add(time.Hour)); st.Stalled {
		t.Fatal("a forgotten server restarts its hold")
	}
}
