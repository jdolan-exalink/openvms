package provisionlpr

import (
	"testing"
	"time"
)

func TestDeduplicatorSuppressesDuplicatesWithinWindow(t *testing.T) {
	d := NewDeduplicator(10 * time.Second)
	t0 := time.Now()

	// First reading: not a duplicate
	if d.IsDuplicate("cam-01", "AF123BC", t0) {
		t.Fatal("expected first reading to not be duplicate")
	}

	// Immediate retry after 2 seconds: duplicate!
	if !d.IsDuplicate("cam-01", "AF123BC", t0.Add(2*time.Second)) {
		t.Fatal("expected duplicate reading within window to be suppressed")
	}

	// Different plate on same camera: not a duplicate
	if d.IsDuplicate("cam-01", "AE456FG", t0.Add(3*time.Second)) {
		t.Fatal("expected different plate on same camera to not be duplicate")
	}

	// Same plate on different camera: not a duplicate
	if d.IsDuplicate("cam-02", "AF123BC", t0.Add(4*time.Second)) {
		t.Fatal("expected same plate on different camera to not be duplicate")
	}

	// Same plate after 12 seconds (> 10s window): legitimate second passage, not a duplicate!
	if d.IsDuplicate("cam-01", "AF123BC", t0.Add(12*time.Second)) {
		t.Fatal("expected reading after window expiration to not be duplicate")
	}
}

func TestDeduplicatorBoundsWindow(t *testing.T) {
	dShort := NewDeduplicator(1 * time.Second)
	if dShort.window != 5*time.Second {
		t.Fatalf("expected min window 5s, got %v", dShort.window)
	}

	dLong := NewDeduplicator(300 * time.Second)
	if dLong.window != 120*time.Second {
		t.Fatalf("expected max window 120s, got %v", dLong.window)
	}
}
