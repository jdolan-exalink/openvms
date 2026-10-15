package visionai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRowToEvents(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	score := float32(0.9)
	row := EventRow{
		ID: uuid.New(), TenantID: uuid.New(), SiteID: uuid.New(), ServerID: uuid.New(), CameraID: uuid.New(),
		CameraName: "Gate", Severity: "detection", Labels: []string{"car", "person"},
		Zones: []string{"street", "Yard"}, Plates: []string{"AB123CD"}, StartTime: now,
		LPRScore: &score, PlatesAllowed: true,
	}
	cfg := DefaultVisionConfig(row.TenantID)

	t.Run("one event per label with lpr data when permitted", func(t *testing.T) {
		evs := RowToEvents(row, cfg)
		if len(evs) != 2 {
			t.Fatalf("got %d events, want 2", len(evs))
		}
		e := evs[0]
		if e.Object != "car" || e.Type != "lpr" || e.Plate != "AB123CD" || e.Confidence != float64(score) {
			t.Fatalf("unexpected event: %+v", e)
		}
		if e.ID != row.ID.String() || e.CameraName != "Gate" || !e.StartTime.Equal(now) || e.Watchlist {
			t.Fatalf("unexpected identity fields: %+v", e)
		}
	})

	t.Run("plates are hidden without lpr.view", func(t *testing.T) {
		r := row
		r.PlatesAllowed = false
		for _, e := range RowToEvents(r, cfg) {
			if e.Plate != "" || e.Type != "detection" || e.Confidence != DefaultConfidence {
				t.Fatalf("plate leaked or wrong defaults: %+v", e)
			}
		}
	})

	t.Run("zone prefers the configured one", func(t *testing.T) {
		c := cfg
		c.Zones = []string{"yard"}
		if z := RowToEvents(row, c)[0].Zone; z != "Yard" {
			t.Fatalf("zone = %q, want Yard", z)
		}
		if z := RowToEvents(row, cfg)[0].Zone; z != "street" {
			t.Fatalf("zone = %q, want first zone street", z)
		}
	})

	t.Run("alarm severity comes from the Frigate review severity only", func(t *testing.T) {
		// Alarms are created per event by rules and often never closed, so an open alarm row must
		// not flag every event as critical; Frigate's alert/detection split is the signal.
		r := row
		r.Severity = "alert"
		if e := RowToEvents(r, cfg)[0]; e.AlarmSeverity != "high" || e.Type != "alarm" {
			t.Fatalf("alert: %+v", e)
		}
		if e := RowToEvents(row, cfg)[0]; e.AlarmSeverity != "" {
			t.Fatalf("plain detection: %+v", e)
		}
	})

	t.Run("rows without labels yield nothing", func(t *testing.T) {
		r := row
		r.Labels = nil
		if got := RowToEvents(r, cfg); len(got) != 0 {
			t.Fatalf("got %d events", len(got))
		}
	})
}

func TestAnyShared(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	cases := map[string]struct {
		events, lpr []uuid.UUID
		want        bool
	}{
		"overlap":            {[]uuid.UUID{a, b}, []uuid.UUID{b, c}, true},
		"disjoint":           {[]uuid.UUID{a}, []uuid.UUID{b, c}, false},
		"no lpr cameras":     {[]uuid.UUID{a}, nil, false},
		"no event cameras":   {nil, []uuid.UUID{a}, false},
		"lpr only elsewhere": {[]uuid.UUID{a}, []uuid.UUID{c}, false},
	}
	for name, tc := range cases {
		if got := anyShared(tc.events, tc.lpr); got != tc.want {
			t.Errorf("%s: anyShared = %v, want %v", name, got, tc.want)
		}
	}
}
