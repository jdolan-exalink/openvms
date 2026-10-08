package events

import (
	"testing"
	"time"
)

func TestParseTracks(t *testing.T) {
	raw := []byte(`[{"object_id":"1700000000.1-abc","label":"car","zones":["driveway"],"box":[0.1,0.2,0.3,0.4],
"path":[{"x":0.5,"y":0.6,"t":1700000000.5},{"x":0.55,"y":0.65,"t":1700000001.5}],
"start_time":"2023-11-14T22:13:20.5Z","end_time":null}]`)
	got, err := parseTracks(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tracks, want 1", len(got))
	}
	tr := got[0]
	if tr.ObjectID != "1700000000.1-abc" || tr.Label != "car" || len(tr.Zones) != 1 || len(tr.Box) != 4 {
		t.Fatalf("unexpected track: %+v", tr)
	}
	if len(tr.Path) != 2 || tr.Path[1].T != 1700000001.5 {
		t.Fatalf("unexpected path: %+v", tr.Path)
	}
	if tr.EndTime != nil || !tr.StartTime.Equal(time.Date(2023, 11, 14, 22, 13, 20, 500000000, time.UTC)) {
		t.Fatalf("unexpected times: %+v", tr)
	}
}

func TestParseTracksEmpty(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(`[]`), []byte(`null`)} {
		got, err := parseTracks(in)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("parseTracks(%q) = %v, %v; want empty non-nil slice", in, got, err)
		}
	}
}
