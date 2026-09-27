package frigatemock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseCameras(t *testing.T) {
	cams, err := ParseCameras("acceso_norte+lpr:entrada|salida, plaza,cementerio:portón")
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 3 {
		t.Fatalf("want 3 cameras, got %d", len(cams))
	}
	if !cams[0].LPR || cams[0].Name != "acceso_norte" || len(cams[0].Zones) != 2 {
		t.Errorf("unexpected first camera: %+v", cams[0])
	}
	if cams[1].LPR || cams[1].Zones != nil {
		t.Errorf("unexpected second camera: %+v", cams[1])
	}
	if _, err := ParseCameras(" , "); err == nil {
		t.Error("empty spec should fail")
	}
}

func TestStoreListFiltersAndOrders(t *testing.T) {
	s := NewStore(10)
	s.Put(Review{ID: "a", Camera: "plaza", StartTime: 100, Severity: "alert"})
	s.Put(Review{ID: "b", Camera: "plaza", StartTime: 300, Severity: "detection"})
	s.Put(Review{ID: "c", Camera: "ruta", StartTime: 200, Severity: "alert"})

	got := s.List(Query{})
	if ids(got) != "b,c,a" {
		t.Errorf("want newest first b,c,a, got %s", ids(got))
	}
	if got := s.List(Query{Cameras: map[string]bool{"plaza": true}}); ids(got) != "b,a" {
		t.Errorf("camera filter: got %s", ids(got))
	}
	if got := s.List(Query{Severity: "alert", After: 150}); ids(got) != "c" {
		t.Errorf("severity+after filter: got %s", ids(got))
	}
	if got := s.List(Query{Limit: 1}); ids(got) != "b" {
		t.Errorf("limit: got %s", ids(got))
	}
}

func TestStoreEvictsOldest(t *testing.T) {
	s := NewStore(2)
	s.Put(Review{ID: "old", StartTime: 1})
	s.Put(Review{ID: "mid", StartTime: 2})
	s.Put(Review{ID: "new", StartTime: 3})
	if _, ok := s.Get("old"); ok {
		t.Error("oldest review should have been evicted")
	}
}

func TestServerRequiresLogin(t *testing.T) {
	store := NewStore(100)
	g := &Generator{Cameras: []Camera{{Name: "plaza"}}, Store: store}
	g.Seed(5, time.Hour, time.Now())
	srv := &Server{Version: "0.17.2-mock", Cameras: g.Cameras, Store: store, User: "admin", Password: "secret", RequireAuth: true}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/review")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request: want 401, got %d", resp.StatusCode)
	}

	resp, err = http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"user":"admin","password":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad password: want 401, got %d", resp.StatusCode)
	}

	resp, err = http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"user":"admin","password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var token string
	for _, c := range resp.Cookies() {
		if c.Name == tokenCookie {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("login did not set the session cookie")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/review?cameras=plaza&limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reviews []Review
	if err := json.NewDecoder(resp.Body).Decode(&reviews); err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 3 {
		t.Errorf("want 3 reviews, got %d", len(reviews))
	}
	for _, r := range reviews {
		if r.EndTime == nil {
			t.Errorf("seeded review %s should be finished", r.ID)
		}
	}
}

func TestRandomPlateFormats(t *testing.T) {
	g := &Generator{}
	for range 200 {
		p := randomPlate(g.random())
		if len(p) != 6 && len(p) != 7 {
			t.Fatalf("unexpected plate %q", p)
		}
	}
}

func ids(rs []Review) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = r.ID
	}
	return strings.Join(s, ",")
}
