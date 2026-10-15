package visionai

import (
	"strings"
	"time"
)

// CameraHistory tracks when a camera was shown and when it exited.
type CameraHistory struct {
	CurrentlyVisible bool
	VisibleSince     time.Time
	LastExitedAt     time.Time
}

// ScoringEngine computes prioritization scores for Vision events.
type ScoringEngine struct {
	objectWeights map[string]float64
}

// NewScoringEngine initializes scoring weights.
func NewScoringEngine() *ScoringEngine {
	return &ScoringEngine{
		objectWeights: map[string]float64{
			"license_plate": 50,
			"face":          45,
			"person":        40,
			"motorcycle":    25,
			"truck":         25,
			"bus":           25,
			"car":           20,
			"bicycle":       15,
		},
	}
}

// CalculateScore evaluates event importance taking into account object type, confidence,
// zones, alarms, watchlists, and display history penalties (cooldown / visible).
func (s *ScoringEngine) CalculateScore(event VisionEvent, history CameraHistory, config VisionConfig, now time.Time) (float64, map[string]float64) {
	breakdown := make(map[string]float64)

	// 1. Base object weight
	weight, ok := s.objectWeights[strings.ToLower(event.Object)]
	if !ok {
		weight = 15.0
	}
	breakdown["object"] = weight

	// 2. Confidence bonus (scale 0..20)
	confBonus := event.Confidence * 20.0
	breakdown["confidence"] = confBonus

	// 3. Zone priority
	if event.Zone != "" && len(config.Zones) > 0 {
		for _, z := range config.Zones {
			if strings.EqualFold(z, event.Zone) {
				breakdown["zone"] = 30.0
				break
			}
		}
	}

	// 4. Alarm severity
	switch strings.ToLower(event.AlarmSeverity) {
	case "critical":
		breakdown["alarm"] = 100.0
	case "high":
		breakdown["alarm"] = 60.0
	case "medium":
		breakdown["alarm"] = 30.0
	case "low":
		breakdown["alarm"] = 10.0
	}

	// 5. Watchlist match
	if event.Watchlist {
		breakdown["watchlist"] = 80.0
	}

	// 6. Already visible penalty
	if history.CurrentlyVisible {
		breakdown["already_visible_penalty"] = -15.0
	}

	// 7. Cooldown penalty
	if !history.LastExitedAt.IsZero() {
		cooldown := time.Duration(config.CooldownSeconds) * time.Second
		if cooldown == 0 {
			cooldown = 10 * time.Second
		}
		if now.Sub(history.LastExitedAt) < cooldown {
			breakdown["cooldown_penalty"] = -50.0
		}
	}

	var total float64
	for _, v := range breakdown {
		total += v
	}

	return total, breakdown
}
