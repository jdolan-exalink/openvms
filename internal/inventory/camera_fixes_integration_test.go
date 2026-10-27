//go:build integration

package inventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

// leakyPaths are stream URLs that carry a credential outside the plain userinfo form.
var leakyPaths = []string{
	"rtsp://10.0.0.5:554/user=admin&password=Qu3ryS3cret&channel=1&stream=0.sdp",
	"rtsp://admin:p@Qu3ryS3cret@10.0.0.5/main",
	"rtsp://admin:pa/Qu3ryS3cret@10.0.0.5/main",
	"rtsp://10.0.0.5:554/main?password=Qu3ryS3cret",
}

func TestConfigTrailDoesNotLeakStreamCredentials(t *testing.T) {
	for _, leaky := range leakyPaths {
		env := demofix.Setup(t)
		eng := newYAMLEngine()
		useYAMLEngine(env, eng)
		cam := env.Cameras["frigate-h01/acceso_norte"]
		ctx := context.Background()
		eng.doc["ffmpeg"] = map[string]any{"retry_interval": 10.0, "inputs": []any{map[string]any{
			"path": leaky, "roles": []any{"detect"}}}}
		sec, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "ffmpeg")
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(sec.YAML, "- detect", "- detect\n      - record", 1)
		val, err := env.Svc.ValidateCameraYAML(ctx, env.Admin, cam.ID, "ffmpeg", edited)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "ffmpeg", YAML: edited}); err != nil {
			t.Fatal(err)
		}
		reader := grantAt(t, env, "hist-reader", cam.TenantID, authz.ServersConfig, authz.CamerasView)
		hist, err := env.Svc.ListCameraConfigHistory(ctx, reader, cam.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		adv, err := env.Svc.GetCameraAdvanced(ctx, env.Admin, cam.ID)
		if err != nil {
			t.Fatal(err)
		}
		for name, v := range map[string]any{"history": hist, "audit": configAudit(t, env, cam.ID), "validate": val, "advanced": adv} {
			raw, _ := json.Marshal(v)
			if bytes.Contains(raw, []byte("Qu3ryS3cret")) {
				t.Errorf("%s leaks the credential of %q: %s", name, leaky, raw)
			}
		}
	}
}

func TestOpenCameraEngineKeepsLegacyViewersReading(t *testing.T) {
	env := demofix.Setup(t)
	useYAMLEngine(env, newYAMLEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	viewer := grantAt(t, env, "cfg-viewer", cam.TenantID, authz.CamerasView)
	if _, err := env.Svc.OpenCameraEngine(ctx, viewer, cam.ID); err != nil {
		t.Fatalf("cameras.view alone must read the config: %v", err)
	}
	ptz := grantAt(t, env, "cfg-viewer-ptz", cam.TenantID, authz.CamerasView, authz.CamerasConfigPTZ)
	if _, err := env.Svc.OpenCameraEngine(ctx, ptz, cam.ID); err != nil {
		t.Fatalf("a granular grant elsewhere must not revoke the legacy read: %v", err)
	}
	denied := grantAt(t, env, "cfg-viewer-deny", cam.TenantID, authz.CamerasView)
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: denied.UserID, Permission: authz.CamerasConfigView,
		Effect: authz.Deny, ScopeType: authz.ScopeTenant, ScopeID: &cam.TenantID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.OpenCameraEngine(ctx, denied, cam.ID); err == nil {
		t.Fatal("an explicit deny of cameras.config.view must refuse the read")
	}
	if _, err := env.Svc.OpenCameraEngine(ctx, grantAt(t, env, "cfg-none", cam.TenantID, authz.CamerasConfigPTZ), cam.ID); err == nil {
		t.Fatal("without cameras.view there is no read")
	}
}
