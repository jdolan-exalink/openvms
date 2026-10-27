//go:build integration

package inventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
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

func grantEffectAt(t *testing.T, env *demofix.Env, user authz.Actor, tenant uuid.UUID, effect authz.Effect, perms ...authz.Permission) {
	t.Helper()
	for _, p := range perms {
		if _, err := env.Svc.CreateGrant(context.Background(), env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: user.UserID, Permission: p,
			Effect: effect, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyFrigatePatchHonoursGranularSections(t *testing.T) {
	env := demofix.Setup(t)
	useYAMLEngine(env, newYAMLEngine())
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	record := map[string]any{"record": map[string]any{"enabled": false}}
	detect := map[string]any{"detect": map[string]any{"enabled": true}}
	other := map[string]any{"birdseye": map[string]any{"enabled": true}}

	legacy := grantAt(t, env, "fp-legacy", cam.TenantID, authz.ServersConfig, authz.CamerasView)
	denied := grantAt(t, env, "fp-deny", cam.TenantID, authz.ServersConfig, authz.CamerasView)
	grantEffectAt(t, env, denied, cam.TenantID, authz.Deny, authz.CamerasConfigRecording)
	granular := grantAt(t, env, "fp-granular", cam.TenantID, authz.ServersConfig, authz.CamerasView, authz.CamerasConfigVideo)
	nobody := grantAt(t, env, "fp-nobody", cam.TenantID, authz.CamerasView, authz.CamerasConfigRecording)

	cases := []struct {
		name  string
		actor authz.Actor
		patch map[string]any
		ok    bool
	}{
		{"legacy holder writes record", legacy, record, true},
		{"legacy holder writes detect", legacy, detect, true},
		{"legacy holder writes an unmapped section", legacy, other, true},
		{"denied recording refuses record", denied, record, false},
		{"denied recording still writes detect", denied, detect, true},
		{"granular video holder refused record", granular, record, false},
		{"granular video holder refused detect", granular, detect, false},
		{"granular video holder writes an unmapped section", granular, other, true},
		{"granular holder without servers.config gains nothing", nobody, record, false},
	}
	for _, c := range cases {
		_, err := env.Svc.PatchCameraFrigateConfig(ctx, c.actor, cam.ID, c.patch)
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, access.ErrForbidden) {
			t.Errorf("%s: err = %v, want forbidden", c.name, err)
		}
	}

	// B2: the YAML editor needs the permission of the section it edits too.
	yamlLegacy := grantAt(t, env, "fy-legacy", cam.TenantID, authz.ServersConfig, authz.ServersConfigSecrets, authz.CamerasView)
	yamlDenied := grantAt(t, env, "fy-deny", cam.TenantID, authz.ServersConfig, authz.ServersConfigSecrets, authz.CamerasView)
	grantEffectAt(t, env, yamlDenied, cam.TenantID, authz.Deny, authz.CamerasConfigEdit)
	if _, err := env.Svc.ApplyCameraYAML(ctx, yamlDenied, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: "fps: 7\n"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("yaml apply with a detect deny = %v, want forbidden", err)
	}
	if _, err := env.Svc.ApplyCameraYAML(ctx, yamlLegacy, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: "fps: 7\n"}); err != nil {
		t.Fatalf("legacy yaml holder must keep working: %v", err)
	}
}
