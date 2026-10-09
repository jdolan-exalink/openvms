package frigate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveEvents returns an adapter whose /api/events answers with the given raw JSON.
func serveEvents(t *testing.T, body string) Adapter {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.18.0-77a66e7"))
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestTrackedObjectBoxAndPathDecoding pins that data.box and data.path_data of a real
// Frigate 0.18 event reach the domain TrackedObject.
func TestTrackedObjectBoxAndPathDecoding(t *testing.T) {
	raw, err := os.ReadFile("testdata/frigate018_event_with_plate.json")
	if err != nil {
		t.Fatal(err)
	}
	a := serveEvents(t, "["+string(raw)+"]")
	objs, err := a.TrackedObjects(context.Background(), ObjectQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want 1 object, got %d", len(objs))
	}
	o := objs[0]
	wantBox := []float64{0.3125, 0.11388888888888889, 0.4234375, 0.4888888888888889}
	if len(o.Box) != 4 {
		t.Fatalf("box: want 4 values, got %v", o.Box)
	}
	for i, v := range wantBox {
		if o.Box[i] != v {
			t.Errorf("box[%d]: want %v, got %v", i, v, o.Box[i])
		}
	}
	if len(o.Path) < 3 {
		t.Fatalf("path: want the fixture's points, got %d", len(o.Path))
	}
	first := o.Path[0]
	if first.X != 0.2594 || first.Y != 0.3083 || first.T != 1790543061.619896 {
		t.Errorf("first path point: got %+v", first)
	}
	if o.Path[2].X != 0.3094 || o.Path[2].Y != 0.3694 {
		t.Errorf("third path point: got %+v", o.Path[2])
	}
}

// TestTrackedObjectMalformedPathEntriesAreSkipped proves a bad path_data entry or box does not
// fail the page: valid points survive and the object is still returned.
func TestTrackedObjectMalformedPathEntriesAreSkipped(t *testing.T) {
	a := serveEvents(t, `[{"id":"o1","camera":"c","label":"person","start_time":10,"end_time":null,
"data":{"box":[0.1,0.2],"path_data":[[[0.1,0.2],11.5],"junk",[[0.3],12],[[0.4,0.5],"x"],[[0.6,0.7],13]]}}]`)
	objs, err := a.TrackedObjects(context.Background(), ObjectQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want 1 object, got %d", len(objs))
	}
	if objs[0].Box != nil {
		t.Errorf("a box without 4 values must be dropped, got %v", objs[0].Box)
	}
	if len(objs[0].Path) != 2 || objs[0].Path[0].T != 11.5 || objs[0].Path[1].X != 0.6 {
		t.Errorf("want only the 2 well-formed points, got %+v", objs[0].Path)
	}
}
