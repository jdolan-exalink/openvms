package events

import (
	"strings"
	"testing"
	"time"
)

func TestTimeFilterStartsInside(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	var b sqlArgs
	addTimeFilter(&b, Filter{From: &from, To: &to})
	got := b.sql()
	if got != "e.start_time >= $1 AND e.start_time < $2" {
		t.Errorf("default filter changed: %q", got)
	}
}

func TestTimeFilterOverlap(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	var both sqlArgs
	addTimeFilter(&both, Filter{From: &from, To: &to, Overlap: true})
	got := both.sql()
	for _, want := range []string{"e.start_time < $2", "e.end_time >= $1", "e.end_time IS NULL AND e.start_time >= $1::timestamptz - interval '3600 seconds'"} {
		if !strings.Contains(got, want) {
			t.Errorf("overlap filter %q lacks %q", got, want)
		}
	}
	if len(both.args) != 2 {
		t.Errorf("want 2 args, got %d", len(both.args))
	}

	var fromOnly sqlArgs
	addTimeFilter(&fromOnly, Filter{From: &from, Overlap: true})
	if s := fromOnly.sql(); strings.Contains(s, "$2") || strings.Contains(s, "e.start_time < $") {
		t.Errorf("from-only overlap must apply just the lower half: %q", s)
	}

	var toOnly sqlArgs
	addTimeFilter(&toOnly, Filter{To: &to, Overlap: true})
	if s := toOnly.sql(); s != "e.start_time < $1" {
		t.Errorf("to-only overlap: %q", s)
	}
}

func TestStaleOpenWindowMatchesSyncer(t *testing.T) {
	if StaleOpenWindow != time.Hour {
		t.Errorf("StaleOpenWindow = %v; the syncer and the manifest assume one hour", StaleOpenWindow)
	}
}
