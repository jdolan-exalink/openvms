package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

type fakeProfiles struct {
	camProfileService
	delErr  error
	created inventory.ProfileInput
	ref     *camconfig.ProfileRef
	patch   camconfig.Patch
	assign  inventory.ProfileAssignInput
}

func (f *fakeProfiles) DeleteConfigProfile(context.Context, authz.Actor, uuid.UUID) error {
	return f.delErr
}

func (f *fakeProfiles) CreateConfigProfile(_ context.Context, _ authz.Actor, in inventory.ProfileInput) (inventory.ProfileView, error) {
	f.created = in
	return inventory.ProfileView{PatchDoc: in.Patch}, nil
}

func (f *fakeProfiles) CameraProfileContext(context.Context, authz.Actor, uuid.UUID) (*camconfig.ProfileRef, camconfig.Patch, error) {
	return f.ref, f.patch, nil
}

func (f *fakeProfiles) ApplyCameraProfile(_ context.Context, _ authz.Actor, _ uuid.UUID, in inventory.ProfileAssignInput, _ inventory.VideoLinker) (inventory.ProfileApplyResult, error) {
	f.assign = in
	return inventory.ProfileApplyResult{}, nil
}

func TestDeleteProfileInUseIs409(t *testing.T) {
	h := &Handlers{camProfile: &fakeProfiles{delErr: &inventory.ProfileInUseError{Cameras: 3}}}
	_, err := h.DeleteCameraConfigProfile(writerCtx(), gen.DeleteCameraConfigProfileRequestObject{ProfileId: uuid.New()})
	code, kind, msg := statusFor(err)
	if code != http.StatusConflict || kind != "profile_in_use" || msg == "" {
		t.Fatalf("got %d %q %q", code, kind, msg)
	}
}

func TestCreateProfileRejectsUnknownFields(t *testing.T) {
	f := &fakeProfiles{}
	h := &Handlers{camProfile: f}
	_, err := h.CreateCameraConfigProfile(writerCtx(), gen.CreateCameraConfigProfileRequestObject{Body: &gen.CameraConfigProfileInput{
		Name: "Exterior", Patch: map[string]any{"zones": []any{}}}})
	if code, _, _ := statusFor(err); code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", code)
	}
	res, err := h.CreateCameraConfigProfile(writerCtx(), gen.CreateCameraConfigProfileRequestObject{Body: &gen.CameraConfigProfileInput{
		Name: "Exterior", Patch: map[string]any{"detection": map[string]any{"enabled": true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(gen.CreateCameraConfigProfile201JSONResponse); !ok || f.created.Name != "Exterior" {
		t.Fatalf("res %T created %+v", res, f.created)
	}
}

func TestUnlinkDefaultsToKeepValues(t *testing.T) {
	f := &fakeProfiles{}
	h := &Handlers{camProfile: f}
	if _, err := h.ApplyCameraProfile(writerCtx(), gen.ApplyCameraProfileRequestObject{CameraId: uuid.New(), Body: &gen.CameraConfigProfileAssign{}}); err != nil {
		t.Fatal(err)
	}
	if f.assign.ProfileID != nil || !f.assign.KeepValues {
		t.Fatalf("assign = %+v", f.assign)
	}
	no := false
	if _, err := h.ApplyCameraProfile(writerCtx(), gen.ApplyCameraProfileRequestObject{CameraId: uuid.New(), Body: &gen.CameraConfigProfileAssign{KeepValues: &no}}); err != nil {
		t.Fatal(err)
	}
	if f.assign.KeepValues {
		t.Fatal("keep_values=false ignored")
	}
}

func TestNormalizedConfigServesProvenanceAndProfile(t *testing.T) {
	on := true
	f := &fakeProfiles{ref: &camconfig.ProfileRef{ID: "p1", Name: "Exterior", Version: 2},
		patch: camconfig.Patch{Detection: &camconfig.DetectionPatch{Enabled: &on}}}
	doc := map[string]any{"detect": map[string]any{"enabled": false}, "record": map[string]any{"enabled": true}}
	h := &Handlers{camConfig: fakeCamConfigSource{ce: camConfigFixture(fakeFrigate{doc: doc})}, camProfile: f}
	res, err := h.GetCameraConfig(writerCtx(), gen.GetCameraConfigRequestObject{CameraId: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := gen.CameraConfig(res.(gen.GetCameraConfig200JSONResponse))
	if cfg.Profile == nil || cfg.Profile.Name != "Exterior" {
		t.Fatalf("profile = %+v", cfg.Profile)
	}
	pv := *cfg.Provenance
	if pv["detection.enabled"].Source != gen.CameraConfigProvenanceSourceProfile || pv["recording.enabled"].Source != gen.CameraConfigProvenanceSourceEngine {
		t.Fatalf("provenance = %+v", pv)
	}
	// The profile wants detection on and the engine has it off: that is drift, not "synced".
	if cfg.State.SyncState == "synced" {
		t.Fatalf("state = %+v", cfg.State)
	}
}
