package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/backup"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

const backupYAML = "cameras:\n  front:\n    ffmpeg:\n      inputs:\n        - path: rtsp://admin:hunter2@10.0.0.5/s\n"

type backupFixture struct {
	mux   *http.ServeMux
	store *tokens.Store
	dir   string
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	store := tokens.Open(filepath.Join(t.TempDir(), "agent.tokens.json"))
	auth := &authenticator{secret: "legacy-secret", tokens: store}
	mux := buildMuxWithAuth(auth, "test", agent.NewSampler(agent.Paths{}), nil, nil, nil, nil, func(*http.Request) error { return nil }, func() {})
	dir := t.TempDir()
	mountBackup(mux, auth, backup.New(dir))
	return &backupFixture{mux: mux, store: store, dir: dir}
}

func (f *backupFixture) do(method, bearer, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/v1/backup/original", strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func putBody(config string) string {
	b, _ := json.Marshal(map[string]string{"config": config, "frigate_version": "0.16.1", "registered_by": "OpenVMS Central"})
	return string(b)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return out
}

func TestBackupRoutesRequireTheControlScope(t *testing.T) {
	f := newBackupFixture(t)
	secret, _, err := f.store.Create("read-only", []tokens.Scope{tokens.ScopeRead}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := f.store.Create("control", []tokens.Scope{tokens.ScopeControl}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if got := f.do(method, "", putBody(backupYAML)).Code; got != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", method, got)
		}
		if got := f.do(method, secret, putBody(backupYAML)).Code; got != http.StatusForbidden {
			t.Errorf("%s with a read-only token = %d, want 403", method, got)
		}
	}
	if got := f.do(http.MethodPut, control, putBody(backupYAML)).Code; got != http.StatusCreated {
		t.Errorf("PUT with the control scope = %d, want 201", got)
	}
	if got := f.do(http.MethodGet, "legacy-secret", "").Code; got != http.StatusOK {
		t.Errorf("GET with the legacy secret = %d, want 200", got)
	}
}

func TestBackupPutThenPutAgainKeepsTheFirst(t *testing.T) {
	f := newBackupFixture(t)
	rec := f.do(http.MethodPut, "legacy-secret", putBody(backupYAML))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first PUT = %d %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	if _, leaked := created["config"]; leaked || created["sha256"] == "" || created["frigate_version"] != "0.16.1" || created["registered_by"] != "OpenVMS Central" || created["saved_at"] == "" {
		t.Fatalf("201 body = %v", created)
	}
	rec = f.do(http.MethodPut, "legacy-secret", putBody("other: 1\n"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second PUT = %d, want 409", rec.Code)
	}
	conflict := decode(t, rec)
	if conflict["code"] != "backup_exists" || conflict["sha256"] != created["sha256"] {
		t.Fatalf("409 body = %v, want backup_exists with the first metadata", conflict)
	}
	if _, leaked := conflict["config"]; leaked || strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("409 must not carry the config: %s", rec.Body.String())
	}
	got := decode(t, f.do(http.MethodGet, "legacy-secret", ""))
	if got["config"] != backupYAML {
		t.Fatalf("the first content was replaced: %v", got["config"])
	}
}

func TestBackupGetShapeAndNoBackup(t *testing.T) {
	f := newBackupFixture(t)
	rec := f.do(http.MethodGet, "legacy-secret", "")
	if rec.Code != http.StatusNotFound || decode(t, rec)["code"] != "no_backup" {
		t.Fatalf("GET without backup = %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	f.do(http.MethodPut, "legacy-secret", putBody(backupYAML))
	got := decode(t, f.do(http.MethodGet, "legacy-secret", ""))
	for _, key := range []string{"config", "saved_at", "frigate_version", "registered_by", "sha256", "size"} {
		if _, ok := got[key]; !ok {
			t.Errorf("GET body lacks %q: %v", key, got)
		}
	}
	if int(got["size"].(float64)) != len(backupYAML) {
		t.Errorf("size = %v", got["size"])
	}
}

func TestBackupDelete(t *testing.T) {
	f := newBackupFixture(t)
	rec := f.do(http.MethodDelete, "legacy-secret", "")
	if rec.Code != http.StatusNotFound || decode(t, rec)["code"] != "no_backup" {
		t.Fatalf("DELETE without backup = %d %s", rec.Code, rec.Body.String())
	}
	f.do(http.MethodPut, "legacy-secret", putBody(backupYAML))
	if got := f.do(http.MethodDelete, "legacy-secret", "").Code; got != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", got)
	}
	if got := f.do(http.MethodGet, "legacy-secret", "").Code; got != http.StatusNotFound {
		t.Fatalf("GET after DELETE = %d, want 404", got)
	}
}

func TestBackupPutRejectsBadBodiesWithoutEchoingThem(t *testing.T) {
	f := newBackupFixture(t)
	huge := putBody("a: " + strings.Repeat("x", backup.MaxConfigBytes) + "\n")
	for name, body := range map[string]string{
		"not json":     "config: hunter2",
		"empty config": putBody(""),
		"not yaml":     putBody("a: [hunter2\n"),
		"not a map":    putBody("- hunter2\n"),
		"oversize":     huge,
		"way oversize": strings.Repeat("x", 64<<20),
		"unknown":      `{"config":"a: 1\n","token":"hunter2"}`,
	} {
		rec := f.do(http.MethodPut, "legacy-secret", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d, want 400", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("%s: the error echoes the config: %s", name, rec.Body.String())
		}
	}
	if got := f.do(http.MethodGet, "legacy-secret", "").Code; got != http.StatusNotFound {
		t.Fatalf("a rejected PUT must store nothing, GET = %d", got)
	}
}
