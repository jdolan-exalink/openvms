package inventory

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignalTrackerHoldAndRecover(t *testing.T) {
	id := uuid.New()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := NewSignalTracker(60 * time.Second)

	// Healthy fps never reports a loss.
	if st := tr.Observe(id, 5, true, now); st != SignalOK {
		t.Fatalf("fps 5: got %v, want SignalOK", st)
	}
	// First zero starts the hold; not confirmed yet.
	if st := tr.Observe(id, 0, true, now.Add(30*time.Second)); st != SignalHolding {
		t.Fatalf("first zero: got %v, want SignalHolding", st)
	}
	if st := tr.Observe(id, 0, true, now.Add(60*time.Second)); st != SignalHolding {
		t.Fatalf("zero for 30s of 60s hold: got %v, want SignalHolding", st)
	}
	// Hold elapsed: confirmed.
	if st := tr.Observe(id, 0.0, true, now.Add(90*time.Second)); st != SignalLost {
		t.Fatalf("zero for 60s: got %v, want SignalLost", st)
	}
	if st := tr.Observe(id, 0, true, now.Add(120*time.Second)); st != SignalLost {
		t.Fatalf("still zero: got %v, want SignalLost", st)
	}
	// Recovery on any fps above epsilon.
	if st := tr.Observe(id, 4.9, true, now.Add(150*time.Second)); st != SignalOK {
		t.Fatalf("recovered: got %v, want SignalOK", st)
	}
	// A new loss restarts the hold from scratch.
	if st := tr.Observe(id, 0, true, now.Add(180*time.Second)); st != SignalHolding {
		t.Fatalf("new zero: got %v, want SignalHolding", st)
	}
}

func TestSignalTrackerUnreportedIsUnknown(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	tr := NewSignalTracker(time.Second)
	tr.Observe(id, 0, true, now)
	if st := tr.Observe(id, 0, false, now.Add(time.Hour)); st != SignalUnknown {
		t.Fatalf("unreported camera: got %v, want SignalUnknown", st)
	}
	// Unknown drops the hold; a later zero starts over.
	if st := tr.Observe(id, 0, true, now.Add(time.Hour+time.Second)); st != SignalHolding {
		t.Fatalf("zero after unknown: got %v, want SignalHolding", st)
	}
}

func TestSignalTrackerEpsilon(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	tr := NewSignalTracker(0)
	if st := tr.Observe(id, 0.04, true, now); st != SignalLost {
		t.Fatalf("fps below epsilon with zero hold: got %v, want SignalLost", st)
	}
}

func TestNoSignalPersistedState(t *testing.T) {
	zero, tiny, five := float32(0), float32(0.04), float32(5)
	cases := []struct {
		name   string
		status string
		fps    *float32
		want   bool
	}{
		{"offline and zero fps", "offline", &zero, true},
		{"offline and near-zero fps", "offline", &tiny, true},
		{"offline but never reported", "offline", nil, false},
		{"online", "online", &five, false},
		{"unknown after server outage", "unknown", &zero, false},
		{"zero fps still in hold keeps online", "online", &zero, false},
	}
	for _, c := range cases {
		if got := NoSignal(c.status, c.fps); got != c.want {
			t.Errorf("%s: NoSignal(%q, %v) = %v, want %v", c.name, c.status, c.fps, got, c.want)
		}
	}
}
