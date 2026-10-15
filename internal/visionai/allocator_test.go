package visionai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSlotAllocator(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	cam1 := uuid.New()
	cam2 := uuid.New()
	cam3 := uuid.New()
	cam4 := uuid.New()
	cam5 := uuid.New()

	cfg := DefaultVisionConfig(tenantID)
	cfg.Layout = "4"
	cfg.MinDisplaySeconds = 5
	cfg.MaxDisplaySeconds = 30
	cfg.CooldownSeconds = 10

	t.Run("allocates top scoring events to empty slots", func(t *testing.T) {
		allocator := NewSlotAllocator(cfg)

		events := []VisionEvent{
			{CameraID: cam1, CameraName: "Cam 1", Object: "person", Confidence: 0.9, Timestamp: now},
			{CameraID: cam2, CameraName: "Cam 2", Object: "car", Confidence: 0.8, Timestamp: now},
		}

		slots := allocator.Allocate(events, now)
		if len(slots) != 4 {
			t.Fatalf("expected 4 slots, got %d", len(slots))
		}
		// Slot 0 should be Cam 1 (person has higher score than car)
		if slots[0].CameraID != cam1 {
			t.Fatalf("expected slot 0 to have cam1, got %v", slots[0].CameraID)
		}
		if slots[1].CameraID != cam2 {
			t.Fatalf("expected slot 1 to have cam2, got %v", slots[1].CameraID)
		}
		if slots[2].CameraID != uuid.Nil || slots[3].CameraID != uuid.Nil {
			t.Fatalf("expected slots 2 and 3 to be empty")
		}
	})

	t.Run("respects pinned cameras so they are not replaced", func(t *testing.T) {
		allocator := NewSlotAllocator(cfg)

		allocator.Allocate([]VisionEvent{
			{CameraID: cam1, CameraName: "Cam 1", Object: "car", Confidence: 0.5, Timestamp: now},
		}, now)

		// Pin slot 0
		allocator.SetPin(0, true)

		// New event arrives with much higher score (person with critical alarm)
		allocator.Allocate([]VisionEvent{
			{CameraID: cam2, CameraName: "Cam 2", Object: "person", Confidence: 0.95, AlarmSeverity: "critical", Timestamp: now.Add(time.Second)},
		}, now.Add(time.Second))

		slots := allocator.Slots()
		// Slot 0 must remain cam1 because it was pinned!
		if slots[0].CameraID != cam1 || !slots[0].Pinned {
			t.Fatalf("expected slot 0 to remain pinned cam1, got %v (pinned=%v)", slots[0].CameraID, slots[0].Pinned)
		}
		// Slot 1 should receive cam2
		if slots[1].CameraID != cam2 {
			t.Fatalf("expected slot 1 to receive cam2, got %v", slots[1].CameraID)
		}
	})

	t.Run("protects camera from replacement before min display time has passed", func(t *testing.T) {
		allocator := NewSlotAllocator(cfg) // MinDisplaySeconds = 5

		allocator.Allocate([]VisionEvent{
			{CameraID: cam1, CameraName: "Cam 1", Object: "person", Confidence: 0.6, Timestamp: now},
			{CameraID: cam2, CameraName: "Cam 2", Object: "person", Confidence: 0.6, Timestamp: now},
			{CameraID: cam3, CameraName: "Cam 3", Object: "person", Confidence: 0.6, Timestamp: now},
			{CameraID: cam4, CameraName: "Cam 4", Object: "person", Confidence: 0.6, Timestamp: now},
		}, now)

		// 2 seconds later (within 5s min display), new event cam5 arrives with slightly higher score
		allocator.Allocate([]VisionEvent{
			{CameraID: cam5, CameraName: "Cam 5", Object: "person", Confidence: 0.8, Timestamp: now.Add(2 * time.Second)},
		}, now.Add(2*time.Second))

		slots := allocator.Slots()
		// None of the 4 slots should be replaced yet because min display time (5s) has not elapsed
		for i, s := range slots {
			if s.CameraID == cam5 {
				t.Fatalf("slot %d was prematurely replaced with cam5 before min display time", i)
			}
		}

		// 6 seconds later (now.Add(6s)), min display time passed, cam5 should now replace one of the slots
		allocator.Allocate([]VisionEvent{
			{CameraID: cam5, CameraName: "Cam 5", Object: "person", Confidence: 0.8, Timestamp: now.Add(6 * time.Second)},
		}, now.Add(6*time.Second))

		hasCam5 := false
		for _, s := range allocator.Slots() {
			if s.CameraID == cam5 {
				hasCam5 = true
				break
			}
		}
		if !hasCam5 {
			t.Fatalf("expected cam5 to be allocated after min display time elapsed")
		}
	})

	t.Run("filters out objects not included in config.Objects", func(t *testing.T) {
		cfgOnlyPerson := cfg
		cfgOnlyPerson.Objects = []string{"person"}
		allocator := NewSlotAllocator(cfgOnlyPerson)

		allocator.Allocate([]VisionEvent{
			{CameraID: cam1, CameraName: "Cam 1", Object: "car", Confidence: 0.99, Timestamp: now},
		}, now)

		if allocator.Slots()[0].CameraID != uuid.Nil {
			t.Fatalf("car should have been ignored when config only allows person")
		}
	})

	t.Run("focus layout initializes 5 slots (1 hero + 4 sub)", func(t *testing.T) {
		cfgFocus := cfg
		cfgFocus.Layout = "focus"
		allocator := NewSlotAllocator(cfgFocus)

		slots := allocator.Slots()
		if len(slots) != 5 {
			t.Fatalf("expected 5 slots for focus mode, got %d", len(slots))
		}
	})

	t.Run("require flags filter plate, zone and alarm events", func(t *testing.T) {
		base := []VisionEvent{
			{ID: "a", CameraID: cam1, Object: "car", Confidence: 0.9, Plate: "AB123CD", StartTime: now},
			{ID: "b", CameraID: cam2, Object: "person", Confidence: 0.9, Zone: "yard", StartTime: now},
			{ID: "c", CameraID: cam3, Object: "person", Confidence: 0.9, AlarmSeverity: "high", StartTime: now},
			{ID: "d", CameraID: cam4, Object: "person", Confidence: 0.9, StartTime: now},
		}
		for name, tc := range map[string]struct {
			mut  func(*VisionConfig)
			want uuid.UUID
		}{
			"plate": {func(c *VisionConfig) { c.RequirePlate = true }, cam1},
			"zone":  {func(c *VisionConfig) { c.RequireZone = true }, cam2},
			"alarm": {func(c *VisionConfig) { c.RequireAlarm = true }, cam3},
		} {
			c := cfg
			c.Objects = nil
			tc.mut(&c)
			slots := NewSlotAllocator(c).Allocate(base, now)
			if slots[0].CameraID != tc.want || slots[1].CameraID != uuid.Nil {
				t.Fatalf("%s: slots = %v / %v, want only %v", name, slots[0].CameraID, slots[1].CameraID, tc.want)
			}
		}
	})

	t.Run("keeps only the best candidate per camera and records its event id", func(t *testing.T) {
		allocator := NewSlotAllocator(cfg)
		slots := allocator.Allocate([]VisionEvent{
			{ID: "low", CameraID: cam1, Object: "car", Confidence: 0.5, StartTime: now},
			{ID: "high", CameraID: cam1, Object: "person", Confidence: 0.9, StartTime: now},
		}, now)
		if slots[0].EventID != "high" || slots[0].Reason.Object != "person" {
			t.Fatalf("slot 0 = event %q object %q, want high/person", slots[0].EventID, slots[0].Reason.Object)
		}
		if slots[1].CameraID != uuid.Nil {
			t.Fatalf("camera must occupy a single slot")
		}
	})

	t.Run("releases unpinned slots past max display with no candidate left", func(t *testing.T) {
		allocator := NewSlotAllocator(cfg)
		allocator.Allocate([]VisionEvent{{ID: "x", CameraID: cam1, Object: "person", Confidence: 0.9, StartTime: now}}, now)
		allocator.SetPin(1, true)

		// Still inside max display: kept even without candidates.
		if got := allocator.Allocate(nil, now.Add(20*time.Second))[0].CameraID; got != cam1 {
			t.Fatalf("slot released too early")
		}
		if got := allocator.Allocate(nil, now.Add(31*time.Second))[0].CameraID; got != uuid.Nil {
			t.Fatalf("expected slot 0 released after max display, got %v", got)
		}
	})
}
