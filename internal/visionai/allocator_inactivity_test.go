package visionai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func inactivityConfig(seconds int) VisionConfig {
	cfg := DefaultVisionConfig(uuid.New())
	cfg.Layout = "4"
	cfg.MinDisplaySeconds = 5
	cfg.MaxDisplaySeconds = 30
	cfg.InactivitySeconds = seconds
	return cfg
}

func endedAt(t time.Time) *time.Time { return &t }

// TestAllocatorReleasesCameraAfterInactivity drops a camera as soon as its last detection is older
// than the inactivity timeout, even before the max display time, like Frigate's birdseye.
func TestAllocatorReleasesCameraAfterInactivity(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(10))
	ev := VisionEvent{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now, Timestamp: now}

	if slots := a.Allocate([]VisionEvent{ev}, now); slots[0].CameraID != cam {
		t.Fatalf("running event must take a slot, got %v", slots[0].CameraID)
	}

	ev.EndTime = endedAt(now.Add(2 * time.Second))
	if slots := a.Allocate([]VisionEvent{ev}, now.Add(11*time.Second)); slots[0].CameraID != cam {
		t.Fatal("camera must stay while its last detection is within the inactivity timeout")
	}
	if slots := a.Allocate([]VisionEvent{ev}, now.Add(13*time.Second)); slots[0].CameraID != uuid.Nil {
		t.Fatalf("camera must leave once inactive for 10s, still in slot 0: %v", slots[0].CameraID)
	}
}

func TestAllocatorIgnoresInactiveCandidates(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	a := NewSlotAllocator(inactivityConfig(10))
	stale := VisionEvent{ID: "old", CameraID: uuid.New(), Object: "person", Confidence: 0.9,
		StartTime: now.Add(-time.Minute), Timestamp: now.Add(-time.Minute), EndTime: endedAt(now.Add(-30 * time.Second))}
	if slots := a.Allocate([]VisionEvent{stale}, now); slots[0].CameraID != uuid.Nil {
		t.Fatal("an event inactive for longer than the timeout must not take a slot")
	}
}

func TestAllocatorKeepsPinnedCameraWhenInactive(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(10))
	a.Allocate([]VisionEvent{{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now, Timestamp: now}}, now)
	a.SetPin(0, true)
	if slots := a.Allocate(nil, now.Add(time.Minute)); slots[0].CameraID != cam {
		t.Fatal("a pinned camera must never be released for inactivity")
	}
}

func TestAllocatorReportsDetectionStart(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(30))
	first := VisionEvent{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now.Add(-4 * time.Second), Timestamp: now}
	if slots := a.Allocate([]VisionEvent{first}, now); !slots[0].DetectedAt.Equal(first.StartTime) {
		t.Fatalf("DetectedAt = %v, want the event start %v", slots[0].DetectedAt, first.StartTime)
	}
	// A newer event on the same camera refreshes the detection time without moving the camera.
	next := VisionEvent{ID: "e2", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now.Add(3 * time.Second), Timestamp: now.Add(3 * time.Second)}
	if slots := a.Allocate([]VisionEvent{next}, now.Add(3*time.Second)); !slots[0].DetectedAt.Equal(next.StartTime) {
		t.Fatalf("DetectedAt = %v, want the newer event start %v", slots[0].DetectedAt, next.StartTime)
	}
}

func TestAllocatorWithoutInactivityKeepsMaxDisplayRelease(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(0))
	a.Allocate([]VisionEvent{{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now, Timestamp: now}}, now)
	if slots := a.Allocate(nil, now.Add(20*time.Second)); slots[0].CameraID != cam {
		t.Fatal("without an inactivity timeout the camera stays until max display")
	}
	if slots := a.Allocate(nil, now.Add(31*time.Second)); slots[0].CameraID != uuid.Nil {
		t.Fatal("without an inactivity timeout the camera leaves after max display")
	}
}

// TestAllocatorInactivityHonorsMinDisplay never flickers a camera out before its min display time.
func TestAllocatorInactivityHonorsMinDisplay(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(10))
	a.Allocate([]VisionEvent{{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now, Timestamp: now}}, now)
	if slots := a.Allocate(nil, now.Add(2*time.Second)); slots[0].CameraID != cam {
		t.Fatal("camera must stay until its min display time even without candidates")
	}
	if slots := a.Allocate(nil, now.Add(6*time.Second)); slots[0].CameraID != uuid.Nil {
		t.Fatal("camera without candidates must leave once its min display time passed")
	}
}

// TestAllocatorReportsLiveActivity marks a camera active while any of its events runs, and otherwise
// reports the latest end across its events.
func TestAllocatorReportsLiveActivity(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(30))
	strong := VisionEvent{ID: "strong", CameraID: cam, Object: "person", Confidence: 0.95, AlarmSeverity: "high",
		StartTime: now.Add(-20 * time.Second), Timestamp: now.Add(-20 * time.Second), EndTime: endedAt(now.Add(-8 * time.Second))}
	running := VisionEvent{ID: "running", CameraID: cam, Object: "car", Confidence: 0.5, StartTime: now.Add(-3 * time.Second), Timestamp: now.Add(-3 * time.Second)}

	slots := a.Allocate([]VisionEvent{strong, running}, now)
	if !slots[0].Active || !slots[0].LastSeenAt.Equal(now) {
		t.Fatalf("a running event on the camera must mark it active now: active=%v last=%v", slots[0].Active, slots[0].LastSeenAt)
	}

	running.EndTime = endedAt(now.Add(2 * time.Second))
	slots = a.Allocate([]VisionEvent{strong, running}, now.Add(5*time.Second))
	if slots[0].Active || !slots[0].LastSeenAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("ended camera must report its latest end: active=%v last=%v", slots[0].Active, slots[0].LastSeenAt)
	}
}

func TestAllocatorClearsActiveWhenCameraHasNoCandidate(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(10))
	a.Allocate([]VisionEvent{{ID: "e1", CameraID: cam, Object: "person", Confidence: 0.9, StartTime: now, Timestamp: now}}, now)
	a.SetPin(0, true)
	slots := a.Allocate(nil, now.Add(time.Minute))
	if slots[0].CameraID != cam || slots[0].Active {
		t.Fatalf("pinned camera without candidates must stay but not be active: cam=%v active=%v", slots[0].CameraID, slots[0].Active)
	}
	if !slots[0].LastSeenAt.Equal(now) {
		t.Fatalf("LastSeenAt = %v, want the last observed activity %v", slots[0].LastSeenAt, now)
	}
}

// TestAllocatorDetectedAtFollowsNewestEvent reports the newest detection on the camera even when an
// older event still wins the slot on score.
func TestAllocatorDetectedAtFollowsNewestEvent(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cam := uuid.New()
	a := NewSlotAllocator(inactivityConfig(30))
	strong := VisionEvent{ID: "strong", CameraID: cam, Object: "person", Confidence: 0.95, AlarmSeverity: "high", StartTime: now.Add(-20 * time.Second), Timestamp: now.Add(-20 * time.Second)}
	weakNewer := VisionEvent{ID: "weak", CameraID: cam, Object: "car", Confidence: 0.4, StartTime: now.Add(-2 * time.Second), Timestamp: now.Add(-2 * time.Second)}
	slots := a.Allocate([]VisionEvent{strong, weakNewer}, now)
	if slots[0].EventID != "strong" {
		t.Fatalf("fixture: the stronger event must hold the slot, got %q", slots[0].EventID)
	}
	if !slots[0].DetectedAt.Equal(weakNewer.StartTime) {
		t.Fatalf("DetectedAt = %v, want the newest event start %v", slots[0].DetectedAt, weakNewer.StartTime)
	}
}
