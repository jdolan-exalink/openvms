package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

const (
	backupSecretYAML = "cameras:\n  front:\n    ffmpeg:\n      inputs:\n        - path: rtsp://admin:hunter2@10.0.0.5/s\n"
	backupMetaJSON   = `{"saved_at":"2026-10-26T10:00:00Z","frigate_version":"0.16.1","registered_by":"OpenVMS","sha256":"abc","size":42}`
	backupFullJSON   = `{"config":"cameras: {}\n","saved_at":"2026-10-26T10:00:00Z","frigate_version":"0.16.1","registered_by":"OpenVMS","sha256":"abc","size":42}`
)

type backupHarness struct {
	*Service
	mu       sync.Mutex
	requests []string
	putBody  string
	audited  []string
	computed int
}

// newBackupHarness answers the agent with reply(method) and records what OpenVMS sent.
func newBackupHarness(reply func(method string) (int, string)) *backupHarness {
	h := &backupHarness{}
	h.Service = videoEncodingService(func(r *http.Request) (*http.Response, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.requests = append(h.requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			h.putBody = string(b)
		}
		status, body := reply(r.Method)
		return agentReply(status, body)(r)
	})
	return h
}

// ensure runs the ensure flow with fakes for the two central lookups (the original config and the audit).
func (h *backupHarness) ensure(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentBackup, error) {
	return h.ensureOriginalConfigBackup(ctx, actor, serverID, backupSources{
		original: func(context.Context, authz.Actor, uuid.UUID) (string, string, error) {
			h.computed++
			return backupSecretYAML, "0.16.1", nil
		},
		audit: func(_ context.Context, _ authz.Actor, _ uuid.UUID, sha string) error {
			h.audited = append(h.audited, sha)
			return nil
		},
	})
}

func TestEnsureBackupExistingIsNotOverwritten(t *testing.T) {
	h := newBackupHarness(func(string) (int, string) { return 200, backupFullJSON })
	got, err := h.ensure(context.Background(), authz.Actor{Username: "ana"}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || got.SHA256 != "abc" || got.FrigateVersion != "0.16.1" || got.Size != 42 || got.SavedAt.IsZero() {
		t.Fatalf("backup = %+v", got)
	}
	if len(h.requests) != 1 || h.requests[0] != "GET /v1/backup/original" || h.computed != 0 || len(h.audited) != 0 {
		t.Fatalf("requests=%v computed=%d audited=%v: an existing backup must cause no PUT", h.requests, h.computed, h.audited)
	}
}

func TestEnsureBackupStoresTheComputedOriginalWhenThereIsNone(t *testing.T) {
	h := newBackupHarness(func(method string) (int, string) {
		if method == http.MethodGet {
			return 404, `{"code":"no_backup"}`
		}
		return 201, backupMetaJSON
	})
	got, err := h.ensure(context.Background(), authz.Actor{Username: "ana"}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || got.SHA256 != "abc" {
		t.Fatalf("backup = %+v", got)
	}
	if len(h.requests) != 2 || h.requests[1] != "PUT /v1/backup/original" {
		t.Fatalf("requests = %v", h.requests)
	}
	for _, want := range []string{`"config":"cameras:\n  front:`, `"frigate_version":"0.16.1"`, `"registered_by":"OpenVMS`, "ana"} {
		if !strings.Contains(h.putBody, want) {
			t.Errorf("PUT body lacks %q: %s", want, h.putBody)
		}
	}
	if len(h.audited) != 1 || h.audited[0] != "abc" {
		t.Fatalf("audit = %v, want one entry with the sha256", h.audited)
	}
}

func TestEnsureBackupLosingTheRaceIsFine(t *testing.T) {
	h := newBackupHarness(func(method string) (int, string) {
		if method == http.MethodGet {
			return 404, `{"code":"no_backup"}`
		}
		return 409, `{"code":"backup_exists","saved_at":"2026-10-26T10:00:00Z","frigate_version":"0.16.1","registered_by":"x","sha256":"first","size":9}`
	})
	got, err := h.ensure(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || !got.Exists || got.SHA256 != "first" {
		t.Fatalf("backup = %+v, %v", got, err)
	}
	if len(h.audited) != 0 {
		t.Fatalf("the loser must not audit: %v", h.audited)
	}
}

func TestEnsureBackupOldAgentIsUnsupported(t *testing.T) {
	for name, status := range map[string]int{"plain 404": 404, "405": 405} {
		h := newBackupHarness(func(string) (int, string) { return status, "404 page not found" })
		_, err := h.ensure(context.Background(), authz.Actor{}, uuid.New())
		if !errors.Is(err, ErrAgentBackupUnsupported) {
			t.Errorf("%s: err = %v, want ErrAgentBackupUnsupported", name, err)
		}
		if h.computed != 0 {
			t.Errorf("%s: nothing should be computed for an agent that cannot store it", name)
		}
	}
	// A PUT that finds no route is the same.
	h := newBackupHarness(func(method string) (int, string) {
		if method == http.MethodGet {
			return 404, `{"code":"no_backup"}`
		}
		return 404, "404 page not found"
	})
	if _, err := h.ensure(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentBackupUnsupported) {
		t.Errorf("PUT 404: err = %v", err)
	}
}

func TestEnsureBackupOtherFailuresAreProbeFailures(t *testing.T) {
	for name, reply := range map[string]func(string) (int, string){
		"agent error": func(string) (int, string) { return 500, "boom" },
		"bad json":    func(string) (int, string) { return 200, "<html>" },
		"forbidden":   func(string) (int, string) { return 403, "this token does not have the control scope" },
		"put rejected": func(m string) (int, string) {
			return map[bool]int{true: 404, false: 400}[m == http.MethodGet], `{"code":"no_backup"}`
		},
	} {
		h := newBackupHarness(reply)
		_, err := h.ensure(context.Background(), authz.Actor{}, uuid.New())
		if !errors.Is(err, ErrAgentProbeFailed) {
			t.Errorf("%s: err = %v, want ErrAgentProbeFailed", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: the error carries the config", name)
		}
	}
}

func TestEnsureBackupNeedsManageAndConfigSecrets(t *testing.T) {
	for name, tc := range map[string]struct{ manage, secrets error }{
		"no manage":  {access.ErrForbidden, nil},
		"no secrets": {nil, access.ErrForbidden},
	} {
		h := newBackupHarness(func(string) (int, string) { return 200, backupFullJSON })
		h.requireServerManage = func(context.Context, authz.Actor, uuid.UUID) error { return tc.manage }
		h.requireServerConfigSecrets = func(context.Context, authz.Actor, uuid.UUID) error { return tc.secrets }
		if _, err := h.ensure(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("%s: err = %v, want forbidden", name, err)
		}
		if len(h.requests) != 0 {
			t.Errorf("%s: the agent was called before authorization: %v", name, h.requests)
		}
	}
}

func TestGetAgentBackupNeverReturnsTheConfigAndCreatesNothing(t *testing.T) {
	h := newBackupHarness(func(string) (int, string) {
		return 200, `{"config":"` + strings.ReplaceAll(backupSecretYAML, "\n", `\n`) + `","saved_at":"2026-10-26T10:00:00Z","frigate_version":"0.16.1","registered_by":"OpenVMS","sha256":"abc","size":42}`
	})
	got, err := h.GetAgentBackup(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || !got.Exists || got.SHA256 != "abc" {
		t.Fatalf("backup = %+v, %v", got, err)
	}
	if strings.Contains(strings.Join([]string{got.FrigateVersion, got.RegisteredBy, got.SHA256}, "|"), "hunter2") {
		t.Fatal("the config leaked")
	}
	if len(h.requests) != 1 || h.computed != 0 {
		t.Fatalf("requests = %v: Get must only read", h.requests)
	}

	none := newBackupHarness(func(string) (int, string) { return 404, `{"code":"no_backup"}` })
	got, err = none.GetAgentBackup(context.Background(), authz.Actor{}, uuid.New())
	if err != nil || got.Exists {
		t.Fatalf("no backup = %+v, %v", got, err)
	}
	old := newBackupHarness(func(string) (int, string) { return 404, "404 page not found" })
	if _, err := old.GetAgentBackup(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, ErrAgentBackupUnsupported) {
		t.Fatalf("old agent = %v", err)
	}
}

func TestGetAgentBackupNeedsServerManage(t *testing.T) {
	h := newBackupHarness(func(string) (int, string) { return 200, backupFullJSON })
	h.requireServerManage = func(context.Context, authz.Actor, uuid.UUID) error { return access.ErrForbidden }
	if _, err := h.GetAgentBackup(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if len(h.requests) != 0 {
		t.Fatalf("the agent was called: %v", h.requests)
	}
}
