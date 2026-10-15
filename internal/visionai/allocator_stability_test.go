package visionai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAllocatorKeepsIncumbentAgainstEqualNewcomers guards against grid churn: a camera on screen
// carries the already-visible penalty, which must not let equally ranked newcomers evict it every
// time the minimum display elapses.
func TestAllocatorKeepsIncumbentAgainstEqualNewcomers(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	cfg := DefaultVisionConfig(uuid.New())
	cfg.Layout = "1"
	cfg.MinDisplaySeconds = 5
	cfg.MaxDisplaySeconds = 30
	incumbent, newcomer := uuid.New(), uuid.New()
	ev := func(cam uuid.UUID, at time.Time) VisionEvent {
		return VisionEvent{ID: cam.String(), CameraID: cam, Object: "person", Confidence: 0.75, StartTime: at, Timestamp: at}
	}

	a := NewSlotAllocator(cfg)
	a.Allocate([]VisionEvent{ev(incumbent, now)}, now)
	// A second poll refreshes the slot score, which now carries the already-visible penalty.
	a.Allocate([]VisionEvent{ev(incumbent, now)}, now.Add(2*time.Second))

	later := now.Add(10 * time.Second) // past min display, before max display
	slots := a.Allocate([]VisionEvent{ev(incumbent, now), ev(newcomer, later)}, later)
	if slots[0].CameraID != incumbent {
		t.Fatalf("equally ranked newcomer evicted the incumbent before max display")
	}

	afterMax := now.Add(31 * time.Second)
	slots = a.Allocate([]VisionEvent{ev(incumbent, now), ev(newcomer, later)}, afterMax)
	if slots[0].CameraID != newcomer {
		t.Fatalf("newcomer should take the slot once the incumbent outlived max display")
	}
}

// TestAllocatorReplacesIncumbentForClearlyHigherScore keeps priority events responsive: a newcomer
// that beats the incumbent by more than the replacement margin takes the slot after min display.
func TestAllocatorReplacesIncumbentForClearlyHigherScore(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	cfg := DefaultVisionConfig(uuid.New())
	cfg.Layout = "1"
	incumbent, urgent := uuid.New(), uuid.New()

	a := NewSlotAllocator(cfg)
	first := []VisionEvent{{ID: "a", CameraID: incumbent, Object: "person", Confidence: 0.75, StartTime: now, Timestamp: now}}
	a.Allocate(first, now)
	a.Allocate(first, now.Add(2*time.Second))

	later := now.Add(10 * time.Second)
	slots := a.Allocate([]VisionEvent{
		{ID: "a", CameraID: incumbent, Object: "person", Confidence: 0.75, StartTime: now, Timestamp: now},
		{ID: "b", CameraID: urgent, Object: "person", Confidence: 0.75, AlarmSeverity: "critical", StartTime: later, Timestamp: later},
	}, later)
	if slots[0].CameraID != urgent {
		t.Fatalf("clearly higher-scoring newcomer should replace the incumbent after min display")
	}
}

// TestAllocatorReplacementMarginBoundary pins the hysteresis: before max display a newcomer needs
// strictly more than replaceMargin over the incumbent's standing score.
func TestAllocatorReplacementMarginBoundary(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	later := now.Add(10 * time.Second) // past min display, before max display
	incumbentKept := func(newcomerConfidence float64) bool {
		cfg := DefaultVisionConfig(uuid.New())
		cfg.Layout = "1"
		incumbent, newcomer := uuid.New(), uuid.New()
		first := []VisionEvent{{ID: "a", CameraID: incumbent, Object: "person", Confidence: 0.25, StartTime: now, Timestamp: now}}
		a := NewSlotAllocator(cfg)
		a.Allocate(first, now)
		slots := a.Allocate(first, now.Add(2*time.Second))
		if p := slots[0].Reason.Breakdown["already_visible_penalty"]; p >= 0 {
			t.Fatalf("already_visible_penalty = %v, want a negative penalty on the on-screen camera", p)
		}
		slots = a.Allocate(append(first, VisionEvent{ID: "b", CameraID: newcomer, Object: "person", Confidence: newcomerConfidence, StartTime: now, Timestamp: now}), later)
		return slots[0].CameraID == incumbent
	}
	if !incumbentKept(0.75) { // confidence 5 -> 15 points: exactly the margin
		t.Fatal("a newcomer exactly at the margin must not evict the incumbent")
	}
	if incumbentKept(0.80) { // 16 points: one point past the margin
		t.Fatal("a newcomer past the margin must evict the incumbent")
	}
}
