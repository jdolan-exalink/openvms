package visionai

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVisionService(t *testing.T) {
	tenantID := uuid.New()
	cam1 := uuid.New()

	svc := NewService(nil)

	ctx := context.Background()

	t.Run("creates default config and returns initialized slots", func(t *testing.T) {
		configs, err := svc.ListConfigs(ctx, tenantID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) < 1 {
			t.Fatalf("expected at least default config, got %d", len(configs))
		}

		def := configs[0]
		slots, err := svc.GetSlots(ctx, tenantID, def.ID)
		if err != nil {
			t.Fatalf("unexpected error getting slots: %v", err)
		}
		if len(slots) != 4 {
			t.Fatalf("expected 4 default slots, got %d", len(slots))
		}
	})

	t.Run("ingests events and updates slots in real time", func(t *testing.T) {
		defConfig, _ := svc.GetDefaultConfig(ctx, tenantID)

		subCh := svc.Subscribe(tenantID, defConfig.ID)

		now := time.Now()
		svc.IngestEvent(ctx, VisionEvent{
			TenantID:   tenantID,
			CameraID:   cam1,
			CameraName: "Cam 1",
			Object:     "person",
			Confidence: 0.95,
			Timestamp:  now,
		})

		select {
		case update := <-subCh:
			if len(update) != 4 {
				t.Fatalf("expected 4 slots in update, got %d", len(update))
			}
			if update[0].CameraID != cam1 {
				t.Fatalf("expected slot 0 to hold cam1, got %v", update[0].CameraID)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for slot update broadcast")
		}
	})

	t.Run("pins a slot through service", func(t *testing.T) {
		defConfig, _ := svc.GetDefaultConfig(ctx, tenantID)

		err := svc.SetPin(ctx, tenantID, defConfig.ID, 0, true)
		if err != nil {
			t.Fatalf("unexpected error pinning slot: %v", err)
		}

		slots, _ := svc.GetSlots(ctx, tenantID, defConfig.ID)
		if !slots[0].Pinned {
			t.Fatalf("expected slot 0 to be pinned")
		}
	})

	t.Run("saves custom configuration and initializes its allocator", func(t *testing.T) {
		custom := VisionConfig{
			TenantID:          tenantID,
			Name:              "Vision AI Perimetro",
			Mode:              "focus",
			Layout:            "focus",
			Objects:           []string{"person"},
			Zones:             []string{"perimeter"},
			MinDisplaySeconds: 10,
			MaxDisplaySeconds: 60,
			CooldownSeconds:   15,
		}

		saved, err := svc.SaveConfig(ctx, custom)
		if err != nil {
			t.Fatalf("unexpected error saving config: %v", err)
		}
		if saved.ID == "" {
			t.Fatalf("expected generated ID")
		}

		slots, err := svc.GetSlots(ctx, tenantID, saved.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(slots) != 5 {
			t.Fatalf("expected 5 slots for focus mode, got %d", len(slots))
		}
	})
}
