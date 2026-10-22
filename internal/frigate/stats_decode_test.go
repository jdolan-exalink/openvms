package frigate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStatsDecodesDetectionEnabled pins that a camera Frigate reports with detection off is
// marked, and that a version that omits the flag counts as enabled.
func TestStatsDecodesDetectionEnabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.18.0-77a66e7"))
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cameras":{
			"off":{"camera_fps":5,"detection_fps":0,"detection_enabled":false},
			"on":{"camera_fps":5,"detection_fps":1.5,"detection_enabled":true},
			"legacy":{"camera_fps":5,"detection_fps":0}},
			"service":{"uptime":1,"version":"0.18.0"}}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	a, err := Connect(context.Background(), ConnInfo{BaseURL: ts.URL, AuthMode: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"off": true, "on": false, "legacy": false} {
		if got := st.Cameras[name].DetectionDisabled; got != want {
			t.Errorf("%s: DetectionDisabled = %v, want %v", name, got, want)
		}
	}
}
