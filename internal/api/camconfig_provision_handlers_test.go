package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

type fakeProvisionAgent struct {
	status map[string]any
	err    error
	calls  int
}

func (f *fakeProvisionAgent) GetProvisionLPRStatus(context.Context, authz.Actor, uuid.UUID) (map[string]any, error) {
	f.calls++
	return f.status, f.err
}

func provisionFixture(ad frigate.Adapter) inventory.CameraEngine {
	ce := camConfigFixture(ad)
	ce.Camera.Tags = []string{"lpr_provision"}
	ce.Camera.OnvifCapabilities = json.RawMessage(`{"manufacturer":"Provision-ISR","model":"I6"}`)
	return ce
}

func getWith(t *testing.T, h *Handlers) gen.CameraConfig {
	t.Helper()
	res, err := h.GetCameraConfig(writerCtx(), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	return gen.CameraConfig(res.(gen.GetCameraConfig200JSONResponse))
}

func integrationStatus(t *testing.T, cfg gen.CameraConfig, id string) string {
	t.Helper()
	for _, i := range cfg.Integrations {
		if i.Id != nil && string(*i.Id) == id {
			return string(*i.Status)
		}
	}
	t.Fatalf("no %s integration: %+v", id, cfg.Integrations)
	return ""
}

func TestGetCameraConfigProvisionOnlyAnswersWithBothSources(t *testing.T) {
	agent := &fakeProvisionAgent{status: map[string]any{"cameras": []any{
		map[string]any{"camera_id": "cam1", "online": true, "event_count": 4.0},
	}}}
	h := &Handlers{camProvision: agent, camConfig: fakeCamConfigSource{ce: provisionFixture(fakeFrigate{docErr: frigate.ErrNotFound})}}
	cfg := getWith(t, h)

	if cfg.Origin.Engine != "provision" {
		t.Fatalf("engine = %s", cfg.Origin.Engine)
	}
	if len(cfg.Sources) != 2 || cfg.Sources[0].Name != "frigate" || cfg.Sources[0].Status != gen.CameraConfigSourcesStatusUnavailable || cfg.Sources[1].Name != "provision" || cfg.Sources[1].Status != gen.CameraConfigSourcesStatusOk {
		t.Fatalf("sources: %+v", cfg.Sources)
	}
	// Vehículos is on, no subtype is invented, and the note says why.
	if cfg.Detection.Notes == nil || (*cfg.Detection.Notes)[0] != "Provision no distingue el tipo de vehículo" {
		t.Fatalf("notes: %+v", cfg.Detection.Notes)
	}
	for _, id := range *cfg.Detection.Selection {
		if strings.HasPrefix(id, "vehicle.") && id != "vehicle.generic" {
			t.Fatalf("invented %s", id)
		}
	}
	if integrationStatus(t, cfg, "provision_isr") != "connected" || integrationStatus(t, cfg, "frigate") != "not_configured" {
		t.Fatalf("integrations: %+v", cfg.Integrations)
	}
	if agent.calls != 1 {
		t.Fatalf("agent asked %d times", agent.calls)
	}
}

func TestGetCameraConfigFrigateAndProvisionMerge(t *testing.T) {
	doc := map[string]any{
		"detect":  map[string]any{"enabled": true},
		"objects": map[string]any{"track": []any{"person", "car"}},
	}
	h := &Handlers{camProvision: &fakeProvisionAgent{err: errors.New("dial tcp 10.1.1.1:7419: refused")}, camConfig: fakeCamConfigSource{ce: provisionFixture(fakeFrigate{doc: doc})}}
	cfg := getWith(t, h)
	if cfg.Origin.Engine != "frigate" {
		t.Fatalf("engine = %s", cfg.Origin.Engine)
	}
	if got := strings.Join(cfg.Detection.Objects, ","); got != "person,car" {
		t.Fatalf("frigate detection must win: %s", got)
	}
	// The agent could not be asked: unknown, and the cause is not leaked.
	if integrationStatus(t, cfg, "provision_isr") != "unknown" || integrationStatus(t, cfg, "frigate") != "connected" {
		t.Fatalf("integrations: %+v", cfg.Integrations)
	}
	raw, _ := json.Marshal(cfg)
	if strings.Contains(string(raw), "10.1.1.1") {
		t.Fatalf("leaked agent error: %s", raw)
	}
}

func TestGetCameraConfigNonProvisionNeverAsksTheAgent(t *testing.T) {
	agent := &fakeProvisionAgent{}
	h := &Handlers{camProvision: agent, camConfig: fakeCamConfigSource{ce: camConfigFixture(fakeFrigate{doc: map[string]any{}})}}
	cfg := getWith(t, h)
	if agent.calls != 0 {
		t.Fatalf("agent asked %d times for a plain Frigate camera", agent.calls)
	}
	if integrationStatus(t, cfg, "provision_isr") != "not_configured" || integrationStatus(t, cfg, "onvif") != "connected" {
		t.Fatalf("integrations: %+v", cfg.Integrations)
	}
}

func TestProvisionOnlyWritesAnswerUnsupported(t *testing.T) {
	provOnly := fakeCamConfigSource{ce: provisionFixture(fakeFrigate{docErr: frigate.ErrNotFound})}
	patch := map[string]any{"detection": map[string]any{"enabled": true}, "recording": map[string]any{"enabled": true}}

	t.Run("review lists the reason and never reaches the service", func(t *testing.T) {
		w := &fakeCamConfigWriter{}
		h := &Handlers{camConfigWrite: w, camConfig: provOnly}
		body := gen.ReviewCameraConfigJSONRequestBody(patch)
		res, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
		if err != nil {
			t.Fatal(err)
		}
		out := res.(gen.ReviewCameraConfig200JSONResponse)
		if len(out.Changes) != 0 || len(out.Unsupported) != 2 || !strings.HasSuffix(out.Unsupported[0], "Se configura en la cámara") || w.reviewCalls != 0 {
			t.Fatalf("review = %+v calls=%d", out, w.reviewCalls)
		}
	})
	t.Run("apply is a 400 with the reason", func(t *testing.T) {
		w := &fakeCamConfigWriter{}
		h := &Handlers{camConfigWrite: w, camConfig: provOnly}
		body := gen.ApplyCameraConfigJSONRequestBody{Patch: patch, ExpectedRevision: 1}
		_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
		status, _, _ := statusFor(err)
		if status != http.StatusBadRequest || !strings.Contains(err.Error(), "Se configura en la cámara") || w.applyCalls != 0 {
			t.Fatalf("status=%d err=%v calls=%d", status, err, w.applyCalls)
		}
	})
	t.Run("a camera Frigate serves still reaches the service", func(t *testing.T) {
		w := &fakeCamConfigWriter{review: camconfig.Review{Changes: []camconfig.Change{{Path: "detection.enabled"}}}}
		h := &Handlers{camConfigWrite: w, camConfig: fakeCamConfigSource{ce: provisionFixture(fakeFrigate{doc: map[string]any{}})}}
		body := gen.ReviewCameraConfigJSONRequestBody(patch)
		if _, err := h.ReviewCameraConfig(writerCtx(), gen.ReviewCameraConfigRequestObject{CameraId: uuid.New(), Body: &body}); err != nil || w.reviewCalls != 1 {
			t.Fatalf("err=%v calls=%d", err, w.reviewCalls)
		}
	})
}

// Permissions are those of the sibling endpoints: whatever opening the camera refuses is
// refused here too, before anything else runs.
func TestProvisionGuardKeepsPermissionErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{"forbidden": {access.ErrForbidden, http.StatusForbidden}, "not found": {store.ErrNotFound, http.StatusNotFound}} {
		t.Run(name, func(t *testing.T) {
			h := &Handlers{camConfigWrite: &fakeCamConfigWriter{}, camConfig: fakeCamConfigSource{err: tc.err}}
			body := gen.ApplyCameraConfigJSONRequestBody{Patch: map[string]any{"detection": map[string]any{"enabled": true}}}
			_, err := h.ApplyCameraConfig(writerCtx(), gen.ApplyCameraConfigRequestObject{CameraId: uuid.New(), Body: &body})
			if status, _, _ := statusFor(err); status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
		})
	}
}
