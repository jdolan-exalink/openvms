package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/selfupdate"
)

func testELF(payload string) []byte {
	h := make([]byte, 64)
	copy(h, "\x7fELF")
	h[4], h[5], h[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(h[18:], 62)
	return append(h, payload...)
}

func TestUpdateEndpointMapsRefusalsToStatusCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"pending update": {selfupdate.ErrUpdatePending, http.StatusConflict},
		"bad payload":    {errors.New("update payload does not match its checksum"), http.StatusBadRequest},
		"accepted":       {nil, http.StatusAccepted},
	} {
		restarted := false
		mux := buildMuxWithAuth(&authenticator{secret: "s"}, "", nil, nil, nil, nil, nil, func(*http.Request) error { return tc.err }, func() { restarted = true })
		req := httptest.NewRequest(http.MethodPost, "/v1/update", strings.NewReader("x"))
		req.Header.Set("Authorization", "Bearer s")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}
		if restarted != (tc.err == nil) {
			t.Errorf("%s: restarted=%v; the agent restarts only for an accepted update", name, restarted)
		}
	}
}

// The replacer reads the checksum and the version OpenVMS expects from headers and refuses a
// payload that does not match them, before anything is installed.
func TestUpdateReplacerEnforcesTheChecksumHeader(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "openvms-agent")
	if err := os.WriteFile(exe, testELF("OLD"), 0o755); err != nil { //nolint:gosec // test files inside t.TempDir()
		t.Fatal(err)
	}
	u := &selfupdate.Updater{ExePath: exe, StateDir: dir, GOARCH: "amd64",
		Run: func(context.Context, string, ...string) ([]byte, error) { return []byte("0.4.1\n"), nil }}
	replace := newUpdateReplacer(u)

	req := httptest.NewRequest(http.MethodPost, "/v1/update", bytes.NewReader(testELF("NEW")))
	req.Header.Set(updateChecksumHeader, strings.Repeat("0", 64))
	if err := replace(req); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum refusal", err)
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, testELF("OLD")) { //nolint:gosec // test files inside t.TempDir()
		t.Fatal("a refused update changed the binary")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/update", bytes.NewReader(testELF("NEW")))
	req.Header.Set(updateVersionHeader, "0.4.1")
	if err := replace(req); err != nil {
		t.Fatalf("a valid update was refused: %v", err)
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, testELF("NEW")) { //nolint:gosec // test files inside t.TempDir()
		t.Fatal("the update was not installed")
	}
}

func TestRollbackApplyRestoresAnUnconfirmedUpdateAndRestarts(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "openvms-agent")
	_ = os.WriteFile(exe, testELF("OLD"), 0o755) //nolint:gosec // test files inside t.TempDir()
	u := &selfupdate.Updater{ExePath: exe, StateDir: dir, GOARCH: "amd64", Now: func() time.Time { return time.Now() },
		Run: func(context.Context, string, ...string) ([]byte, error) { return []byte("0.4.1\n"), nil }}
	if _, err := u.Apply(context.Background(), testELF("NEW"), "", ""); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	restarted := 0
	args := []string{"--state-dir", dir, "--exe", exe}
	if code := rollbackApply(args, &out, &errb, func(context.Context) error { restarted++; return nil }); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, testELF("OLD")) || restarted != 1 { //nolint:gosec // test files inside t.TempDir()
		t.Fatalf("binary restored=%v restarts=%d", bytes.Equal(got, testELF("OLD")), restarted)
	}
	// Confirmed or already rolled back: nothing to do, and no restart.
	out.Reset()
	if code := rollbackApply(args, &out, &errb, func(context.Context) error { restarted++; return nil }); code != 0 || restarted != 1 || !strings.Contains(out.String(), "nothing to roll back") {
		t.Fatalf("second run: exit %d restarts %d out %q", code, restarted, out.String())
	}
	if code := rollbackApply([]string{"--state-dir", dir}, &out, &errb, nil); code != 2 {
		t.Fatalf("missing --exe: exit %d, want 2", code)
	}
}
