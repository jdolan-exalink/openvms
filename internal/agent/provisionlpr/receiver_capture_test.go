package provisionlpr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newCapturingReceiver(t *testing.T, all bool) (*Receiver, *CaptureStore) {
	t.Helper()
	store, err := NewCaptureStore(filepath.Join(t.TempDir(), "captures"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReceiver(DefaultConfig(), nil)
	r.SetCaptures(store, all)
	return r, store
}

func postTo(r *Receiver, path, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.RemoteAddr = "10.0.15.107:54321"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestReceiverKeepsAnUnparseablePayloadAndStillAnswers200(t *testing.T) {
	r, store := newCapturingReceiver(t, false)
	body := []byte{0x01, 0x02, 0xfe, 0xff, 'n', 'o', ' ', 'p', 'l', 'a', 't', 'e'}

	if rec := postTo(r, "/Vendor/NewEvent", "application/octet-stream", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the camera does not retry", rec.Code)
	}
	list, _ := store.List(0)
	if len(list) != 1 || list[0].Reason != CaptureUnparsed || list[0].Path != "/Vendor/NewEvent" || list[0].SourceIP != "10.0.15.107" {
		t.Fatalf("captures = %+v, want the one unparsed request", list)
	}
	if _, kept, _ := store.Get(list[0].ID); !bytes.Equal(kept, body) {
		t.Fatalf("kept body = %x, want %x", kept, body)
	}
}

func TestReceiverKeepsParsedEventsOnlyWhenCaptureAllIsOn(t *testing.T) {
	event := []byte(`{"plate":"AF123BC","camera_id":"cam-01"}`)

	r, store := newCapturingReceiver(t, false)
	postTo(r, "/v1/provision-lpr/event", "application/json", event)
	if list, _ := store.List(0); len(list) != 0 {
		t.Fatalf("a parsed event was captured without capture-all: %+v", list)
	}

	r, store = newCapturingReceiver(t, true)
	postTo(r, "/v1/provision-lpr/event", "application/json", event)
	list, _ := store.List(0)
	if len(list) != 1 || list[0].Reason != CaptureAll || list[0].ParseError != "" {
		t.Fatalf("captures = %+v, want the parsed event kept as capture_all", list)
	}
}

func TestReceiverNeverCapturesKeepalives(t *testing.T) {
	r, store := newCapturingReceiver(t, true)
	if rec := postTo(r, "/SendKeepalive", "application/xml; charset=utf-8", nil); rec.Code != http.StatusOK {
		t.Fatalf("keepalive status = %d", rec.Code)
	}
	if list, _ := store.List(0); len(list) != 0 {
		t.Fatalf("a keepalive was captured: %+v", list)
	}
}

func TestReceiverServesCapturesAndRefusesOtherMethodsAndBadIDs(t *testing.T) {
	r, store := newCapturingReceiver(t, false)
	body := []byte("<unknown>vendor payload</unknown>\x00")
	postTo(r, "/Vendor/NewEvent", "application/octet-stream", body)
	list, _ := store.List(0)
	if len(list) != 1 {
		t.Fatalf("setup: %d captures", len(list))
	}
	id := list[0].ID
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	var listed struct{ Captures []Capture }
	if rec := get("/v1/provision-lpr/captures"); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &listed) != nil || len(listed.Captures) != 1 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	var meta Capture
	if rec := get("/v1/provision-lpr/captures/" + id); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &meta) != nil || meta.ID != id {
		t.Fatalf("meta = %d %s", rec.Code, rec.Body.String())
	}
	rec := get("/v1/provision-lpr/captures/" + id + "/body")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), body) ||
		rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("body = %d %q headers %v", rec.Code, rec.Body.Bytes(), rec.Header())
	}
	for _, path := range []string{"/v1/provision-lpr/captures/nope", "/v1/provision-lpr/captures/../status", "/v1/provision-lpr/captures/20261023T180005123456789Z-aaaaaaaaaaaa/body"} {
		if got := get(path).Code; got != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, got)
		}
	}
	del := httptest.NewRecorder()
	r.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/v1/provision-lpr/captures/"+id, nil))
	if del.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d, want 405", del.Code)
	}
}

func TestReceiverWithoutCapturesReportsThemAsDisabled(t *testing.T) {
	r := NewReceiver(DefaultConfig(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/provision-lpr/captures", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when captures are off", rec.Code)
	}
}

func TestReceiverAnswers200EvenWhenACaptureCannotBeStored(t *testing.T) {
	r, store := newCapturingReceiver(t, false)
	if err := os.RemoveAll(store.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- postTo(r, "/Vendor/NewEvent", "application/octet-stream", []byte{0xff, 0xfe}).Code }()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200 even though the capture failed", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receiver blocked on a failing capture")
	}
}
