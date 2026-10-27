package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

type fakeAdvanced struct {
	section  inventory.YAMLSection
	val      inventory.YAMLValidation
	apply    inventory.YAMLApplyResult
	applyErr error
	history  []inventory.ConfigHistoryEntry
	rollback inventory.RollbackResult
	adv      inventory.CameraAdvanced
	gotApply inventory.YAMLApplyInput
	gotRoll  inventory.RollbackInput
}

func (f *fakeAdvanced) GetCameraYAMLSection(context.Context, authz.Actor, uuid.UUID, string) (inventory.YAMLSection, error) {
	return f.section, nil
}
func (f *fakeAdvanced) ValidateCameraYAML(context.Context, authz.Actor, uuid.UUID, string, string) (inventory.YAMLValidation, error) {
	return f.val, nil
}
func (f *fakeAdvanced) ApplyCameraYAML(_ context.Context, _ authz.Actor, _ uuid.UUID, in inventory.YAMLApplyInput) (inventory.YAMLApplyResult, error) {
	f.gotApply = in
	return f.apply, f.applyErr
}
func (f *fakeAdvanced) ListCameraConfigHistory(context.Context, authz.Actor, uuid.UUID, int) ([]inventory.ConfigHistoryEntry, error) {
	return f.history, nil
}
func (f *fakeAdvanced) RollbackCameraConfig(_ context.Context, _ authz.Actor, _ uuid.UUID, in inventory.RollbackInput, _ inventory.VideoLinker) (inventory.RollbackResult, error) {
	f.gotRoll = in
	return f.rollback, nil
}
func (f *fakeAdvanced) GetCameraAdvanced(context.Context, authz.Actor, uuid.UUID) (inventory.CameraAdvanced, error) {
	return f.adv, nil
}

func advCtx() context.Context {
	return WithActor(context.Background(), authz.Actor{UserID: uuid.New(), Username: "ana"})
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFrigateYamlEndpointsShapes(t *testing.T) {
	cam := uuid.New()
	f := &fakeAdvanced{
		section: inventory.YAMLSection{
			CameraID: cam, Section: "detect", YAML: "enabled: true\n", Exists: true, SecretsVisible: false, Revision: 4,
			ManagedPaths: []camconfig.ManagedPath{{PatchPath: "detection.enabled", EnginePath: "enabled", Value: true}},
		},
		val: inventory.YAMLValidation{
			Section: "detect", Valid: false, SchemaChecked: true, Revision: 4,
			Syntax: []camconfig.SyntaxIssue{{Line: 2, Column: 3, Message: "bad"}}, Schema: []camconfig.SchemaIssue{{Path: "fps", Message: "too big"}},
			Changes:          []camconfig.LeafChange{{Path: "enabled", Kind: camconfig.LeafChanged, From: true, To: false}},
			ManagedConflicts: []camconfig.ManagedConflict{{PatchPath: "detection.enabled", EnginePath: "enabled", Desired: true, New: false}},
			Warnings:         []string{"w"},
		},
		apply: inventory.YAMLApplyResult{
			Validation: inventory.YAMLValidation{Section: "detect", Valid: true},
			Patch:      inventory.FrigatePatchResult{RevisionID: uuid.New(), Sections: []frigate.SectionResult{{Section: "detect", AppliedLive: true}}},
			Marked:     true, Revision: 5, SyncState: "drift",
		},
	}
	h := &Handlers{camAdvanced: f}

	gres, err := h.GetCameraFrigateYaml(advCtx(), gen.GetCameraFrigateYamlRequestObject{CameraId: cam, Params: gen.GetCameraFrigateYamlParams{Section: "detect"}})
	if err != nil {
		t.Fatal(err)
	}
	got := jsonOf(t, gres)
	for _, frag := range []string{`"yaml":"enabled: true\n"`, `"secrets_visible":false`, `"revision":4`, `"patch_path":"detection.enabled"`, `"engine_path":"enabled"`} {
		if !strings.Contains(got, frag) {
			t.Errorf("section response lacks %s: %s", frag, got)
		}
	}

	vres, err := h.ValidateCameraFrigateYaml(advCtx(), gen.ValidateCameraFrigateYamlRequestObject{CameraId: cam, Body: &gen.ValidateCameraFrigateYamlJSONRequestBody{Section: "detect", Yaml: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	got = jsonOf(t, vres)
	for _, frag := range []string{`"valid":false`, `"line":2`, `"column":3`, `"path":"fps"`, `"kind":"changed"`, `"managed_conflicts":[{`, `"new":false`, `"schema_checked":true`} {
		if !strings.Contains(got, frag) {
			t.Errorf("validation response lacks %s: %s", frag, got)
		}
	}

	t3 := true
	ares, err := h.ApplyCameraFrigateYaml(advCtx(), gen.ApplyCameraFrigateYamlRequestObject{CameraId: cam, Body: &gen.ApplyCameraFrigateYamlJSONRequestBody{
		Section: "detect", Yaml: "enabled: false\n", ExpectedRevision: 4, ConfirmManagedConflicts: &t3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotApply.Section != "detect" || f.gotApply.ExpectedRevision != 4 || !f.gotApply.ConfirmManagedConflicts {
		t.Fatalf("service input = %+v", f.gotApply)
	}
	got = jsonOf(t, ares)
	for _, frag := range []string{`"marked_drift":true`, `"revision":5`, `"sync_state":"drift"`, `"applied_live":true`} {
		if !strings.Contains(got, frag) {
			t.Errorf("apply response lacks %s: %s", frag, got)
		}
	}
	if _, err := h.ApplyCameraFrigateYaml(advCtx(), gen.ApplyCameraFrigateYamlRequestObject{CameraId: cam}); err == nil {
		t.Fatal("a missing body must be refused")
	}
}

func TestManagedConflictAnswers409AndRevisionConflict409(t *testing.T) {
	err := &inventory.ManagedConflictError{Conflicts: []camconfig.ManagedConflict{{PatchPath: "detection.enabled", EnginePath: "enabled"}}}
	code, errCode, msg := statusFor(err)
	if code != http.StatusConflict || errCode != "managed_conflict" || !strings.Contains(msg, "enabled") {
		t.Fatalf("managed conflict -> %d %s %s", code, errCode, msg)
	}
	if code, errCode, _ := statusFor(inventory.ErrRevisionConflict); code != http.StatusConflict || errCode != "revision_conflict" {
		t.Fatalf("revision conflict -> %d %s", code, errCode)
	}
}

func TestHistoryRollbackAndPlainErrorShapes(t *testing.T) {
	cam := uuid.New()
	uid := uuid.New()
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	f := &fakeAdvanced{
		history: []inventory.ConfigHistoryEntry{
			{ID: 12, At: at, UserID: &uid, UserName: "ana", Origin: "ui", Result: "ok", Rollbackable: true,
				Changes: []inventory.HistoryChange{{Path: "detection.enabled", From: false, To: true}}},
			{ID: 11, At: at, UserName: "bot", Origin: "yaml", Result: "error", Section: "detect", RollbackReason: "los cambios hechos con YAML se restauran editando el YAML",
				Error:   &camconfig.PlainError{Message: "No se pudo iniciar el stream de grabación.", Technical: "ffmpeg exited with code 1"},
				Changes: []inventory.HistoryChange{{Path: "detect.onvif.password", From: "old", To: "new"}}},
		},
		rollback: inventory.RollbackResult{AuditID: 12, Apply: inventory.ConfigApplyResult{
			Outcome: camconfig.Outcome{SyncState: camconfig.SyncError, LastError: "ffmpeg exited with code 1", Result: camconfig.ResultError,
				Plain: camconfig.PlainError{Message: "No se pudo iniciar el stream de grabación.", Technical: "ffmpeg exited with code 1"}},
			State:  camconfig.State{DesiredRevision: 7, SyncState: camconfig.SyncError, LastError: "ffmpeg exited with code 1", Drift: []camconfig.Drift{}},
			Review: camconfig.Review{Changes: []camconfig.Change{}, Impact: []camconfig.ImpactItem{}, Unsupported: []string{}},
		}},
	}
	h := &Handlers{camAdvanced: f}

	lres, err := h.ListCameraConfigHistory(advCtx(), gen.ListCameraConfigHistoryRequestObject{CameraId: cam})
	if err != nil {
		t.Fatal(err)
	}
	got := jsonOf(t, lres)
	for _, frag := range []string{`"id":12`, `"rollbackable":true`, `"rollbackable":false`, `"rollback_reason":"los cambios hechos con YAML`, `"origin":"yaml"`, `"time":"2026-10-26T12:00:00Z"`, `"from":false`, `"message":"No se pudo iniciar el stream de grabación."`} {
		if !strings.Contains(got, frag) {
			t.Errorf("history lacks %s: %s", frag, got)
		}
	}
	// Credentials in a recorded value never reach the response, whatever the audit holds.
	if strings.Contains(got, `"old"`) || strings.Contains(got, `"new"`) {
		t.Errorf("history leaks a password value: %s", got)
	}

	id := int64(12)
	rres, err := h.RollbackCameraConfig(advCtx(), gen.RollbackCameraConfigRequestObject{CameraId: cam, Body: &gen.RollbackCameraConfigJSONRequestBody{AuditId: &id, ExpectedRevision: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotRoll.AuditID != 12 || f.gotRoll.ExpectedRevision != 6 {
		t.Fatalf("service input = %+v", f.gotRoll)
	}
	got = jsonOf(t, rres)
	for _, frag := range []string{`"rolled_back":12`, `"result":"error"`, `"plain_error":{"message":"No se pudo iniciar el stream de grabación.","technical":"ffmpeg exited with code 1"}`, `"last_error":"ffmpeg exited with code 1"`} {
		if !strings.Contains(got, frag) {
			t.Errorf("rollback response lacks %s: %s", frag, got)
		}
	}
	if _, err := h.RollbackCameraConfig(advCtx(), gen.RollbackCameraConfigRequestObject{CameraId: cam}); err == nil {
		t.Fatal("a missing body must be refused")
	}
}

type fakeAgent struct{ status map[string]any }

func (f fakeAgent) GetProvisionLPRStatus(context.Context, authz.Actor, uuid.UUID) (map[string]any, error) {
	return f.status, nil
}

func TestCameraAdvancedShapeAndProvisionLink(t *testing.T) {
	cam, server := uuid.New(), uuid.New()
	f := &fakeAdvanced{adv: inventory.CameraAdvanced{
		CameraID: cam,
		Frigate:  inventory.AdvancedFrigate{Available: true, Version: "0.17.0", Sections: []string{"detect"}, ManagedSections: []string{"detect"}, DesiredRevision: 2, SyncState: "synced"},
		ONVIF: inventory.AdvancedONVIF{
			Configured: true, Endpoint: "http://192.168.1.50/onvif", Manufacturer: "Provision-ISR",
			Profiles:     []inventory.AdvancedONVIFProfile{{Token: "p1", Name: "main", Encoding: "H264", Width: 1920, Height: 1080, FPS: 25, StreamURI: "rtsp://192.168.1.50:554/main"}},
			Capabilities: map[string]bool{"has_ptz": false}, Services: []string{"ns"}, Reported: map[string]any{"profiles": []any{"Profile S"}},
		},
		Provision: inventory.AdvancedProvision{Evidence: true, Tags: []string{"lpr_provision"}, Manufacturer: "Provision-ISR"},
		Inventory: camconfig.Inventory{CameraID: cam.String(), RemoteName: "cam1", Name: "Cam 1", ServerID: server.String(), ONVIFEndpoint: "http://192.168.1.50/onvif"},
	}}
	status := map[string]any{"cameras": []any{map[string]any{"camera_id": "cam1", "online": true, "event_count": 7.0}}}
	h := &Handlers{camAdvanced: f, camProvision: fakeAgent{status: status}}

	res, err := h.GetCameraAdvanced(advCtx(), gen.GetCameraAdvancedRequestObject{CameraId: cam})
	if err != nil {
		t.Fatal(err)
	}
	got := jsonOf(t, res)
	for _, frag := range []string{
		`"stream_uri":"rtsp://192.168.1.50:554/main"`, `"managed_sections":["detect"]`, `"has_ptz":false`,
		`"registered":true`, `"online":true`, `"event_count":7`, `"reachable":true`, `"evidence":true`,
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("advanced response lacks %s: %s", frag, got)
		}
	}
	// Without Provision evidence the agent is never asked.
	f.adv.Provision.Evidence = false
	h.camProvision = nil
	res, _ = h.GetCameraAdvanced(advCtx(), gen.GetCameraAdvancedRequestObject{CameraId: cam})
	if got := jsonOf(t, res); !strings.Contains(got, `"reachable":false`) {
		t.Errorf("no evidence must report an unreachable link: %s", got)
	}
}
