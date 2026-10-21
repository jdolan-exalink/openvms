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
