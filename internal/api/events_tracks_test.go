package api

import (
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/events"
)

func TestToEventMapsTracks(t *testing.T) {
	end := time.Date(2023, 11, 14, 22, 13, 30, 0, time.UTC)
	e := events.Event{Tracks: []events.Track{{
		ObjectID: "obj-1", Label: "person", Zones: []string{"yard"}, Box: []float64{0.1, 0.2, 0.3, 0.4},
		Path:      []events.TrackPoint{{X: 0.5, Y: 0.6, T: 1700000000.5}},
		StartTime: end.Add(-10 * time.Second), EndTime: &end,
	}}}
	out := toEvent(e)
	if len(out.Tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(out.Tracks))
	}
	tr := out.Tracks[0]
	if tr.ObjectId != "obj-1" || tr.Label != "person" || len(tr.Zones) != 1 {
		t.Fatalf("unexpected track: %+v", tr)
	}
	if tr.Box == nil || len(*tr.Box) != 4 || (*tr.Box)[2] != 0.3 {
		t.Fatalf("unexpected box: %v", tr.Box)
	}
	if len(tr.Path) != 1 || tr.Path[0].T != 1700000000.5 || tr.EndTime == nil {
		t.Fatalf("unexpected path/end: %+v", tr)
	}
}

func TestToEventTracksNeverNil(t *testing.T) {
	if out := toEvent(events.Event{}); out.Tracks == nil {
		t.Fatal("Tracks must be an empty array, not null")
	}
}

func TestToEventSourceFollowsTheOrigin(t *testing.T) {
	cases := []struct {
		name, remoteID, want string
	}{
		{"read by a Provision camera through the agent", "agent:7f3a", "provision"},
		{"review of a Frigate server", "1699999999.123-abc", "frigate"},
		{"no remote id", "", "frigate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := toEvent(events.Event{RemoteID: c.remoteID})
			if out.Source == nil || string(*out.Source) != c.want {
				t.Fatalf("source = %v, want %q", out.Source, c.want)
			}
		})
	}
}
