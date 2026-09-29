package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newWahaClient(url string) *wahaClient {
	return &wahaClient{base: url, key: "k", http: NewHTTPClient(5 * time.Second)}
}

func TestWahaSessionStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions/default" || r.Header.Get("X-Api-Key") != "k" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "default", "status": "WORKING", "me": map[string]any{"id": "5491155555555@c.us", "pushName": "VMS"}})
	}))
	defer srv.Close()
	st, err := newWahaClient(srv.URL).Session(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "default" || st.Status != "WORKING" || st.Phone != "5491155555555" {
		t.Fatalf("session = %+v", st)
	}
}

func TestWahaSessionMissingIsNotFoundStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	st, err := newWahaClient(srv.URL).Session(context.Background(), "default")
	if err != nil || st.Status != "NOT_FOUND" {
		t.Fatalf("session = %+v, %v; want NOT_FOUND status", st, err)
	}
}

func TestWahaUnreachableIsReported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if _, err := newWahaClient(url).Session(context.Background(), "default"); err == nil || !isWahaUnavailable(err) {
		t.Fatalf("err = %v, want a WAHA unavailable error", err)
	}
}

func TestWahaQR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/default/auth/qr" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"mimetype": "image/png", "data": "iVBORw0KGgo="})
	}))
	defer srv.Close()
	qr, err := newWahaClient(srv.URL).QR(context.Background(), "default")
	if err != nil || qr.Mimetype != "image/png" || qr.Data != "iVBORw0KGgo=" {
		t.Fatalf("qr = %+v, %v", qr, err)
	}
}

func TestWahaQRUnavailableWhenNotWaitingForScan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"not in SCAN_QR_CODE"}`, http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	if _, err := newWahaClient(srv.URL).QR(context.Background(), "default"); !errors.Is(err, ErrQRUnavailable) {
		t.Fatalf("err = %v, want ErrQRUnavailable", err)
	}
}

func TestWahaStartCreatesMissingSession(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/default/start":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = body["name"] == "default"
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "default", "status": "STARTING"})
		default:
			http.Error(w, "bad", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	st, err := newWahaClient(srv.URL).Start(context.Background(), "default")
	if err != nil || !created || st.Status != "STARTING" {
		t.Fatalf("start = %+v, %v, created=%v", st, err, created)
	}
}
