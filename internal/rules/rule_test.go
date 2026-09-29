package rules_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/rules"
)

func TestConditionsMatchesEvent(t *testing.T) {
	cam1 := uuid.New()
	cam2 := uuid.New()

	c := rules.Conditions{
		CameraIDs:  []uuid.UUID{cam1},
		Severities: []string{"alert"},
		Labels:     []string{"person", "car"},
		Zones:      []string{"driveway"},
	}

	// Exact match
	ev := rules.EventContext{
		CameraID: cam1,
		Severity: "alert",
		Labels:   []string{"person"},
		Zones:    []string{"driveway"},
	}
	if !c.MatchesEvent(ev) {
		t.Fatal("expected event to match conditions")
	}

	// Different camera
	evWrongCam := ev
	evWrongCam.CameraID = cam2
	if c.MatchesEvent(evWrongCam) {
		t.Fatal("did not expect event with different camera to match")
	}

	// Different severity
	evWrongSev := ev
	evWrongSev.Severity = "detection"
	if c.MatchesEvent(evWrongSev) {
		t.Fatal("did not expect event with different severity to match")
	}

	// Different label
	evWrongLabel := ev
	evWrongLabel.Labels = []string{"dog"}
	if c.MatchesEvent(evWrongLabel) {
		t.Fatal("did not expect event with non-matching label to match")
	}

	// Different zone
	evWrongZone := ev
	evWrongZone.Zones = []string{"backyard"}
	if c.MatchesEvent(evWrongZone) {
		t.Fatal("did not expect event with non-matching zone to match")
	}

	// Empty conditions match any event
	empty := rules.Conditions{}
	if !empty.MatchesEvent(evWrongCam) {
		t.Fatal("expected empty conditions to match any event")
	}
}

func TestConditionsMatchesCameraOffline(t *testing.T) {
	cam1 := uuid.New()
	cam2 := uuid.New()

	c := rules.Conditions{
		CameraIDs:       []uuid.UUID{cam1},
		DurationSeconds: 300,
	}

	if !c.MatchesCameraOffline(cam1, 5*time.Minute) {
		t.Fatal("expected camera offline to match")
	}

	if c.MatchesCameraOffline(cam1, 4*time.Minute) {
		t.Fatal("expected camera offline below duration threshold to not match")
	}

	if c.MatchesCameraOffline(cam2, 10*time.Minute) {
		t.Fatal("expected different camera to not match")
	}
}

func TestConditionsMatchesServerOffline(t *testing.T) {
	srv1 := uuid.New()
	srv2 := uuid.New()

	c := rules.Conditions{
		ServerIDs:       []uuid.UUID{srv1},
		DurationSeconds: 120,
	}

	if !c.MatchesServerOffline(srv1, 2*time.Minute) {
		t.Fatal("expected server offline to match")
	}

	if c.MatchesServerOffline(srv1, 1*time.Minute) {
		t.Fatal("expected server offline below duration to not match")
	}

	if c.MatchesServerOffline(srv2, 5*time.Minute) {
		t.Fatal("expected different server to not match")
	}
}
