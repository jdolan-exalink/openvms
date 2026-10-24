package provisionlpr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type registryFixture struct {
	r      *Receiver
	reg    *Registry
	store  *CaptureStore
	mu     sync.Mutex
	events []LPREvent
}

func newRegistryFixture(t *testing.T) *registryFixture {
	t.Helper()
	reg, err := LoadRegistry(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewCaptureStore(filepath.Join(t.TempDir(), "captures"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := &registryFixture{r: NewReceiver(DefaultConfig(), nil), reg: reg, store: store}
	f.r.SetRegistry(reg)
	f.r.SetCaptures(store, false)
	f.r.Subscribe(func(ev LPREvent) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events = append(f.events, ev)
	})
	return f
}

func (f *registryFixture) emitted() []LPREvent {
	time.Sleep(60 * time.Millisecond) // the subscriber runs on the receiver's own goroutine
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]LPREvent(nil), f.events...)
}

func (f *registryFixture) send(method, path, remote, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec
}

const lprBody = `{"plate":"AF123BC","camera_id":"id-the-payload-claims"}`

func TestRegisteredCameraEventCarriesTheRegisteredIDAndKeepsWhatThePayloadClaimed(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "11111111-2222-3333-4444-555555555555", IP: "10.0.15.107"})

	if rec := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.107:5000", lprBody, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := f.emitted()
	if len(got) != 1 || got[0].CameraID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("events = %+v, want one with the registered camera ID", got)
	}
	if got[0].ReportedCameraID == nil || *got[0].ReportedCameraID != "id-the-payload-claims" {
		t.Fatalf("reported camera id = %v, want the payload's claim kept for audit", got[0].ReportedCameraID)
	}
}

func TestRegisteredCameraKeepaliveIsFiledUnderItsRegisteredID(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "cam-lpr-01", IP: "10.0.15.107"})
	if rec := f.send(http.MethodPost, "/SendKeepalive?camera_id=something-else", "10.0.15.107:5000", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if st, ok := f.r.Heartbeat().Status("cam-lpr-01", "10.0.15.107", time.Now()); !ok || !st.Online {
		t.Fatalf("status = %+v, %v; want the camera online under its registered ID", st, ok)
	}
}

func TestWithoutEnforcementUnknownCamerasAreAcceptedButListed(t *testing.T) {
	f := newRegistryFixture(t)
	if rec := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.200:5000", lprBody, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the legacy behaviour (accepted)", rec.Code)
	}
	if got := f.emitted(); len(got) != 1 || got[0].CameraID != "id-the-payload-claims" {
		t.Fatalf("events = %+v, want the event accepted as before", got)
	}
	seen := f.reg.Snapshot().Unregistered
	if len(seen) != 1 || seen[0].IP != "10.0.15.200" || seen[0].Rejected {
		t.Fatalf("unregistered sources = %+v, want the address listed and not rejected", seen)
	}
}

func TestEnforcementRefusesUnregisteredCamerasAndKeepsTheRequestAsEvidence(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.15.107"})
	_ = f.reg.SetEnforce(true)

	rec := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.200:5000", lprBody, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got := f.emitted(); len(got) != 0 {
		t.Fatalf("events = %+v, an unregistered camera injected an event", got)
	}
	list, _ := f.store.List(0)
	if len(list) != 1 || list[0].Reason != CaptureUnregistered || list[0].SourceIP != "10.0.15.200" {
		t.Fatalf("captures = %+v, want the refused request kept as unregistered", list)
	}
	if keep := f.send(http.MethodPost, "/SendKeepalive", "10.0.15.200:5000", "", nil); keep.Code != http.StatusForbidden {
		t.Fatalf("keepalive status = %d, want 403", keep.Code)
	}
	if _, ok := f.r.Heartbeat().Status("", "10.0.15.200", time.Now()); ok {
		t.Fatal("a refused keepalive marked the address online")
	}
	if seen := f.reg.Snapshot().Unregistered; len(seen) != 1 || !seen[0].Rejected || seen[0].Requests != 2 {
		t.Fatalf("unregistered sources = %+v", seen)
	}
	if ok := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.107:5000", lprBody, nil); ok.Code != http.StatusOK {
		t.Fatalf("registered camera status = %d, want 200", ok.Code)
	}
	if got := f.emitted(); len(got) != 1 || got[0].CameraID != "cam-a" {
		t.Fatalf("events = %+v, want the registered camera's event", got)
	}
}

func TestAForwardedForHeaderCannotMakeAnUnregisteredHostLookRegistered(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.15.107"})
	_ = f.reg.SetEnforce(true)
	rec := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.200:5000", lprBody,
		map[string]string{"X-Forwarded-For": "10.0.15.107", "X-Real-IP": "10.0.15.107", "Forwarded": "for=10.0.15.107"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: headers must not choose the identity", rec.Code)
	}
	if got := f.emitted(); len(got) != 0 {
		t.Fatalf("events = %+v, a spoofed header injected an event", got)
	}
}

func TestRegistryThatFailedToLoadRefusesEveryone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, _ := LoadRegistry(path)
	r := NewReceiver(DefaultConfig(), nil)
	r.SetRegistry(reg)
	req := httptest.NewRequest(http.MethodPost, "/v1/provision-lpr/event", strings.NewReader(lprBody))
	req.RemoteAddr = "10.0.15.107:5000"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 while the registry is unreadable", rec.Code)
	}
}

func TestStatusReportsTheRegistryAndStaysCompatible(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "cam-a", IP: "10.0.15.107"})
	f.send(http.MethodPost, "/SendKeepalive", "10.0.15.107:5000", "", nil)
	rec := f.send(http.MethodGet, "/v1/provision-lpr/status", "127.0.0.1:1", "", nil)
	var out struct {
		Cameras  []CameraStatus   `json:"cameras"`
		Registry RegistrySnapshot `json:"registry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Cameras) != 1 || out.Cameras[0].CameraID != "cam-a" {
		t.Fatalf("status = %s (%v)", rec.Body.String(), err)
	}
	if len(out.Registry.Cameras) != 1 || out.Registry.Enforce {
		t.Fatalf("registry in status = %+v", out.Registry)
	}
	plain := NewReceiver(DefaultConfig(), nil)
	rec = httptest.NewRecorder()
	plain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/provision-lpr/status", nil))
	if strings.Contains(rec.Body.String(), "registry") {
		t.Fatalf("a receiver without a registry reported one: %s", rec.Body.String())
	}
}

func TestRegistryRoutesRegisterEnforceAndRemove(t *testing.T) {
	f := newRegistryFixture(t)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		return f.send(method, path, "127.0.0.1:1", body, nil)
	}
	if rec := call(http.MethodPost, "/v1/provision-lpr/registry/cameras", `{"camera_id":"cam-a","ip":"10.0.15.107"}`); rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/v1/provision-lpr/registry/cameras", `{"camera_id":"cam-b","ip":"10.0.15.107"}`); rec.Code != http.StatusConflict {
		t.Fatalf("same address twice = %d, want 409", rec.Code)
	}
	for name, body := range map[string]string{
		"bad ip":        `{"camera_id":"cam-c","ip":"nope"}`,
		"unknown field": `{"camera_id":"cam-c","ip":"10.0.0.9","enforce":true}`,
		"not json":      `camera`,
		"empty":         ``,
	} {
		if rec := call(http.MethodPost, "/v1/provision-lpr/registry/cameras", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", name, rec.Code)
		}
	}
	if rec := call(http.MethodPut, "/v1/provision-lpr/registry/enforce", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("enforce without a value = %d, want 400", rec.Code)
	}
	if rec := call(http.MethodPut, "/v1/provision-lpr/registry/enforce", `{"enforce":true}`); rec.Code != http.StatusOK || !f.reg.Enforcing() {
		t.Fatalf("enforce = %d, enforcing %v", rec.Code, f.reg.Enforcing())
	}
	var snap RegistrySnapshot
	if rec := call(http.MethodGet, "/v1/provision-lpr/registry", ""); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &snap) != nil || !snap.Enforce || len(snap.Cameras) != 1 {
		t.Fatalf("registry = %d %+v", rec.Code, snap)
	}
	if rec := call(http.MethodDelete, "/v1/provision-lpr/registry/cameras/cam-a", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := call(http.MethodDelete, "/v1/provision-lpr/registry/cameras/..%2Fx", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete with a bad id = %d, want 404", rec.Code)
	}
	for method, path := range map[string]string{
		http.MethodPost:   "/v1/provision-lpr/registry",
		http.MethodGet:    "/v1/provision-lpr/registry/cameras",
		http.MethodPatch:  "/v1/provision-lpr/registry/enforce",
		http.MethodDelete: "/v1/provision-lpr/registry",
	} {
		if rec := call(method, path, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405", method, path, rec.Code)
		}
	}
	if rec := call(http.MethodGet, "/v1/provision-lpr/registry/unknown", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown registry route = %d, want 404", rec.Code)
	}
}

func TestRegistryRoutesAreAbsentWithoutARegistryAndRefuseALockedOne(t *testing.T) {
	plain := NewReceiver(DefaultConfig(), nil)
	rec := httptest.NewRecorder()
	plain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/provision-lpr/registry", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 without a registry", rec.Code)
	}

	path := filepath.Join(t.TempDir(), "registry.json")
	_ = os.WriteFile(path, []byte("{broken"), 0o600)
	locked, _ := LoadRegistry(path)
	r := NewReceiver(DefaultConfig(), nil)
	r.SetRegistry(locked)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/provision-lpr/registry/cameras", `{"camera_id":"cam","ip":"10.0.0.5"}`},
		{http.MethodPut, "/v1/provision-lpr/registry/enforce", `{"enforce":false}`},
		{http.MethodDelete, "/v1/provision-lpr/registry/cameras/cam", ``},
	} {
		req := httptest.NewRequest(c.method, c.path, bytes.NewReader([]byte(c.body)))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s %s on a locked registry = %d, want 409", c.method, c.path, rec.Code)
		}
	}
	if data, _ := os.ReadFile(path); string(data) != "{broken" { //nolint:gosec // temp file created by this test.
		t.Fatalf("the damaged registry was overwritten: %q", data)
	}
}
