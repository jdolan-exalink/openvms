package visionai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestScoringEngine(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	cam1 := uuid.New()
	cam2 := uuid.New()

	engine := NewScoringEngine()

	cfg := DefaultVisionConfig(uuid.New())

	t.Run("calculates base score for person with high confidence", func(t *testing.T) {
		event := VisionEvent{
			ID:         "ev-1",
			CameraID:   cam1,
			CameraName: "Entrada Norte",
			Object:     "person",
			Confidence: 0.95,
			Timestamp:  now,
		}

		score, breakdown := engine.CalculateScore(event, CameraHistory{}, cfg, now)
		// person = 40, confidence 0.95 * 20 = 19 -> 59
		if score != 59.0 {
			t.Fatalf("expected score 59.0, got %f", score)
		}
		if breakdown["object"] != 40.0 || breakdown["confidence"] != 19.0 {
			t.Fatalf("unexpected breakdown: %+v", breakdown)
		}
	})

	t.Run("adds zone bonus when event is in restricted zone", func(t *testing.T) {
		event := VisionEvent{
			ID:         "ev-2",
			CameraID:   cam1,
			Object:     "person",
			Confidence: 0.90,
			Zone:       "perimeter",
			Timestamp:  now,
		}

		cfgWithZone := cfg
		cfgWithZone.Zones = []string{"perimeter"}

		score, breakdown := engine.CalculateScore(event, CameraHistory{}, cfgWithZone, now)
		// person 40 + confidence 18 + zone 30 = 88
		if score != 88.0 {
			t.Fatalf("expected score 88.0, got %f", score)
		}
		if breakdown["zone"] != 30.0 {
			t.Fatalf("expected zone bonus 30.0, got %f", breakdown["zone"])
		}
	})

	t.Run("adds alarm priority bonus according to severity", func(t *testing.T) {
		event := VisionEvent{
			ID:            "ev-3",
			CameraID:      cam1,
			Object:        "person",
			Confidence:    0.80,
			AlarmSeverity: "critical",
			Timestamp:     now,
		}

		score, breakdown := engine.CalculateScore(event, CameraHistory{}, cfg, now)
		// person 40 + conf 16 + critical alarm 100 = 156
		if score != 156.0 {
			t.Fatalf("expected score 156.0, got %f", score)
		}
		if breakdown["alarm"] != 100.0 {
			t.Fatalf("expected alarm bonus 100.0, got %f", breakdown["alarm"])
		}
	})

	t.Run("adds LPR and watchlist bonus", func(t *testing.T) {
		event := VisionEvent{
			ID:         "ev-4",
			CameraID:   cam2,
			Object:     "license_plate",
			Confidence: 0.90,
			Plate:      "AB123CD",
			Watchlist:  true,
			Timestamp:  now,
		}

		score, breakdown := engine.CalculateScore(event, CameraHistory{}, cfg, now)
		// license_plate 50 + conf 18 + watchlist 80 = 148
		if score != 148.0 {
			t.Fatalf("expected score 148.0, got %f", score)
		}
		if breakdown["watchlist"] != 80.0 {
			t.Fatalf("expected watchlist bonus 80.0, got %f", breakdown["watchlist"])
		}
	})

	t.Run("applies penalty if camera is already visible in another slot", func(t *testing.T) {
		event := VisionEvent{
			ID:         "ev-5",
			CameraID:   cam1,
			Object:     "person",
			Confidence: 0.90,
			Timestamp:  now,
		}
		hist := CameraHistory{
			CurrentlyVisible: true,
		}

		score, breakdown := engine.CalculateScore(event, hist, cfg, now)
		// 40 + 18 - 15 = 43
		if score != 43.0 {
			t.Fatalf("expected score 43.0, got %f", score)
		}
		if breakdown["already_visible_penalty"] != -15.0 {
			t.Fatalf("expected visible penalty -15.0, got %f", breakdown["already_visible_penalty"])
		}
	})

	t.Run("applies cooldown penalty if camera recently exited screen", func(t *testing.T) {
		event := VisionEvent{
			ID:         "ev-6",
			CameraID:   cam1,
			Object:     "person",
			Confidence: 0.90,
			Timestamp:  now,
		}
		hist := CameraHistory{
			LastExitedAt: now.Add(-5 * time.Second), // exited 5s ago, cooldown is 10s
		}

		score, breakdown := engine.CalculateScore(event, hist, cfg, now)
		// 40 + 18 - 50 = 8
		if score != 8.0 {
			t.Fatalf("expected score 8.0, got %f", score)
		}
		if breakdown["cooldown_penalty"] != -50.0 {
			t.Fatalf("expected cooldown penalty -50.0, got %f", breakdown["cooldown_penalty"])
		}
	})
}
