//go:build integration

package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

const yamlTestSchema = `{"$defs":{"CameraConfig":{"type":"object","properties":{
 "detect":{"type":"object","additionalProperties":false,"properties":{"enabled":{"type":"boolean"},"fps":{"type":"integer","minimum":1,"maximum":60},"width":{"type":"integer"},"height":{"type":"integer"}}},
 "record":{"type":"object","properties":{"enabled":{"type":"boolean"}}},
 "lpr":{"type":"object","properties":{"enabled":{"type":"boolean"}}},
 "objects":{"type":"object","properties":{"track":{"type":"array","items":{"type":"string"}}}},
 "ffmpeg":{"type":"object","properties":{"inputs":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"roles":{"type":"array","items":{"type":"string"}}}}}}},
 "onvif":{"type":"object","properties":{"host":{"type":"string"},"user":{"type":"string"},"password":{"type":"string"}}},
 "zones":{"type":"object","additionalProperties":{"type":"object"}}
}}}}`

// advUserinfo is built at run time so the fixtures hold no credential-looking literal.
const advUserinfo = "admin" + ":" + "hunter2" + "@"

const (
	realPath     = "rtsp://" + advUserinfo + "10.0.0.5:554/main"
	maskedPath   = "rtsp://*:*@10.0.0.5:554/main"
	realPassword = "s3cret-pass"
)

// yamlEngine is the fake Frigate with a schema and a masked view of the credentials.
type yamlEngine struct {
	*fakeEngine
	secretsErr error
}

func newYAMLEngine() *yamlEngine {
	e := &yamlEngine{fakeEngine: newFakeEngine()}
	e.doc["objects"] = map[string]any{"track": []any{"person"}}
	e.doc["ffmpeg"] = map[string]any{"retry_interval": 10.0, "inputs": []any{map[string]any{"path": realPath, "roles": []any{"detect"}}}}
	e.doc["onvif"] = map[string]any{"host": "10.0.0.5", "user": "admin", "password": realPassword}
	e.doc["zones"] = map[string]any{"ovms_door": map[string]any{"coordinates": "0,0,1,1"}, "mine": map[string]any{}}
	return e
}

func (e *yamlEngine) ConfigSchema(context.Context) (json.RawMessage, error) {
	return json.RawMessage(yamlTestSchema), nil
}

func (e *yamlEngine) CameraConfig(ctx context.Context, name string, secrets bool) (map[string]any, error) {
	if secrets && e.secretsErr != nil {
		return nil, e.secretsErr
	}
	doc, err := e.fakeEngine.CameraConfig(ctx, name, secrets)
	if err != nil || secrets {
		return doc, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	masked := map[string]any{}
	for k, v := range doc {
		raw, _ := json.Marshal(v)
		var c any
		_ = json.Unmarshal(raw, &c)
		masked[k] = c
	}
	if ff, ok := masked["ffmpeg"].(map[string]any); ok {
		for _, in := range ff["inputs"].([]any) {
			in.(map[string]any)["path"] = maskedPath
		}
	}
	if on, ok := masked["onvif"].(map[string]any); ok {
		on["user"], on["password"] = "********", "********"
	}
	return masked, nil
}

func useYAMLEngine(env *demofix.Env, e *yamlEngine) {
	env.Svc.Connect = func(context.Context, frigate.ConnInfo) (frigate.Adapter, error) { return e, nil }
}

func (e *yamlEngine) section(name string) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, _ := e.doc[name].(map[string]any)
	return m
}

func TestCameraYAMLGetShowsSectionManagedPathsAndRevision(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}
	sec, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "detect")
	if err != nil {
		t.Fatal(err)
	}
	if sec.YAML != "enabled: true\n" || !sec.Exists || sec.Revision != 1 || !sec.SecretsVisible {
		t.Fatalf("section = %+v", sec)
	}
	if len(sec.ManagedPaths) != 1 || sec.ManagedPaths[0].PatchPath != "detection.enabled" || sec.ManagedPaths[0].EnginePath != "enabled" {
		t.Fatalf("managed = %+v", sec.ManagedPaths)
	}
	zones, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "zones")
	if err != nil || len(zones.ManagedPaths) != 1 || zones.ManagedPaths[0].EnginePath != "ovms_door" {
		t.Fatalf("zones managed = %+v (%v)", zones.ManagedPaths, err)
	}
	// The administrator sees the credentials; the same call on a masked view shows placeholders.
	ff, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "ffmpeg")
	if err != nil || !strings.Contains(ff.YAML, realPath) {
		t.Fatalf("ffmpeg yaml = %q (%v)", ff.YAML, err)
	}
	eng.secretsErr = errors.New("admin endpoints unavailable")
	ff, err = env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "ffmpeg")
	if err != nil || ff.SecretsVisible || strings.Contains(ff.YAML, "hunter2") || !strings.Contains(ff.YAML, maskedPath) {
		t.Fatalf("masked ffmpeg yaml = %q secrets=%v (%v)", ff.YAML, ff.SecretsVisible, err)
	}
	if _, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "nope"); err == nil {
		t.Fatal("an unknown section must be refused")
	}
	var ve *inventory.ValidationError
	if _, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "../etc"); !errors.As(err, &ve) {
		t.Fatalf("bad section name err = %v", err)
	}
}

func TestCameraYAMLValidateReportsSyntaxSchemaDiffAndConflicts(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}
	patches := eng.patches

	val, err := env.Svc.ValidateCameraYAML(ctx, env.Admin, cam.ID, "detect", "enabled: true\nfps: [1, 2\n")
	if err != nil || val.Valid || len(val.Syntax) != 1 || val.Syntax[0].Line == 0 || val.Syntax[0].Column == 0 {
		t.Fatalf("syntax: %+v (%v)", val, err)
	}
	val, err = env.Svc.ValidateCameraYAML(ctx, env.Admin, cam.ID, "detect", "enabled: true\nfps: 500\nmystery: 1\n")
	if err != nil || val.Valid || !val.SchemaChecked || len(val.Schema) != 2 {
		t.Fatalf("schema: %+v (%v)", val, err)
	}
	val, err = env.Svc.ValidateCameraYAML(ctx, env.Admin, cam.ID, "detect", "enabled: false\nfps: 7\n")
	if err != nil || !val.Valid {
		t.Fatalf("valid edit: %+v (%v)", val, err)
	}
	paths := map[string]string{}
	for _, c := range val.Changes {
		paths[c.Path] = c.Kind
	}
	if paths["enabled"] != "changed" || paths["fps"] != "added" || len(paths) != 2 {
		t.Fatalf("diff = %+v", val.Changes)
	}
	if len(val.ManagedConflicts) != 1 || val.ManagedConflicts[0].PatchPath != "detection.enabled" || val.ManagedConflicts[0].Desired != true || val.ManagedConflicts[0].New != false {
		t.Fatalf("conflicts = %+v", val.ManagedConflicts)
	}
	if eng.patches != patches {
		t.Fatal("validate must not write")
	}
	// Setting the value OpenVMS wants is not a conflict.
	val, _ = env.Svc.ValidateCameraYAML(ctx, env.Admin, cam.ID, "detect", "enabled: true\nfps: 7\n")
	if len(val.ManagedConflicts) != 0 {
		t.Fatalf("conflicts = %+v", val.ManagedConflicts)
	}
}

func TestCameraYAMLApplyGoesThroughPatchAndAuditsYAMLOrigin(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	res, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: "enabled: false\nfps: 7\nwidth: 640\n"})
	if err != nil {
		t.Fatal(err)
	}
	det := eng.section("detect")
	if det["fps"] != float64(7) || det["width"] != float64(640) || det["enabled"] != false {
		t.Fatalf("engine detect = %v", det)
	}
	if eng.patches != 1 || len(res.Patch.Sections) != 1 || res.Patch.Sections[0].Section != "detect" || res.Marked {
		t.Fatalf("patch = %+v marked=%v patches=%d", res.Patch, res.Marked, eng.patches)
	}
	rows := configAudit(t, env, cam.ID)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d", len(rows))
	}
	d := rows[0].Details
	prev, _ := d["previous_values"].(map[string]any)
	next, _ := d["new_values"].(map[string]any)
	if d["origin"] != "yaml" || d["result"] != "ok" || d["section"] != "detect" || next["detect.fps"] != float64(7) || prev["detect.fps"] != nil || prev["detect.enabled"] != nil && prev["detect.enabled"] != false {
		t.Fatalf("audit = %+v", d)
	}
	if _, has := next["detect.enabled"]; has {
		t.Fatal("an unchanged value is not part of the audit diff")
	}
	// A YAML edit is a Frigate revision too: the existing PATCH path ran.
	revs, err := env.Svc.ListFrigateConfigRevisions(ctx, env.Admin, cam.ServerID, &cam.ID, nil, 10, false)
	if err != nil || len(revs) != 1 || revs[0].Kind != inventory.RevisionCameraPatch {
		t.Fatalf("revisions = %+v (%v)", revs, err)
	}
}

func TestCameraYAMLManagedConflictRefusedUnlessConfirmedAndMarksDrift(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	if _, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()}); err != nil {
		t.Fatal(err)
	}
	patches := eng.patches

	in := inventory.YAMLApplyInput{Section: "detect", YAML: "enabled: false\n", ExpectedRevision: 1}
	_, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, in)
	var mce *inventory.ManagedConflictError
	if !errors.As(err, &mce) || len(mce.Conflicts) != 1 || mce.Conflicts[0].PatchPath != "detection.enabled" {
		t.Fatalf("unconfirmed err = %v", err)
	}
	if eng.patches != patches || eng.section("detect")["enabled"] != true {
		t.Fatal("a refused edit must not write")
	}
	stale := in
	stale.ExpectedRevision = 0
	stale.ConfirmManagedConflicts = true
	if _, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, stale); !errors.Is(err, inventory.ErrRevisionConflict) {
		t.Fatalf("stale revision err = %v", err)
	}

	in.ConfirmManagedConflicts = true
	res, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Marked || res.SyncState != camconfig.SyncDrift || res.Revision != 2 || eng.section("detect")["enabled"] != false {
		t.Fatalf("result = %+v", res)
	}
	ce, err := env.Svc.OpenCameraEngine(ctx, env.Admin, cam.ID)
	if err != nil || ce.State == nil || ce.State.SyncState != camconfig.SyncDrift || !strings.Contains(ce.State.LastError, "detection.enabled") {
		t.Fatalf("stored state = %+v (%v)", ce.State, err)
	}
	// OpenVMS keeps wanting detection on: the edit is drift, not the new desired state.
	stored := inventory.StoredConfigState(ce.State)
	if stored.Desired.Detection == nil || *stored.Desired.Detection.Enabled != true {
		t.Fatalf("desired = %+v", stored.Desired)
	}
	rows := configAudit(t, env, cam.ID)
	last := rows[len(rows)-1].Details
	conf, _ := last["managed_conflicts"].([]any)
	if last["origin"] != "yaml" || last["marked_drift"] != true || last["confirmed_managed_conflicts"] != true || len(conf) != 1 {
		t.Fatalf("audit = %+v", last)
	}
}

func TestCameraYAMLInvalidAndEmptyEditsWriteNothing(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	for name, yaml := range map[string]string{
		"syntax": "enabled: [", "schema": "fps: 500\n", "nochange": "enabled: false\n", "empty": "",
	} {
		var ve *inventory.ValidationError
		_, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: yaml})
		if !errors.As(err, &ve) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
	if eng.patches != 0 || len(configAudit(t, env, cam.ID)) != 0 {
		t.Fatalf("invalid edits wrote something: patches=%d", eng.patches)
	}
}

func TestCameraYAMLMaskedPlaceholderNeverOverwritesRealSecret(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	eng.secretsErr = errors.New("admin endpoints unavailable") // only the masked view exists
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	// A placeholder in a map is dropped: the engine merges, so the real password stays.
	sec, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "onvif")
	if err != nil || !strings.Contains(sec.YAML, "********") {
		t.Fatalf("masked onvif = %q (%v)", sec.YAML, err)
	}
	edited := strings.Replace(sec.YAML, "host: 10.0.0.5", "host: 10.0.0.9", 1)
	if _, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "onvif", YAML: edited}); err != nil {
		t.Fatal(err)
	}
	on := eng.section("onvif")
	if on["host"] != "10.0.0.9" || on["password"] != realPassword || on["user"] != "admin" {
		t.Fatalf("engine onvif = %v", on)
	}

	// A placeholder inside a list cannot be kept without the real value: refused, nothing written.
	ff, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	patches := eng.patches
	var ve *inventory.ValidationError
	_, err = env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "ffmpeg", YAML: strings.Replace(ff.YAML, "- detect", "- detect\n      - record", 1)})
	if !errors.As(err, &ve) || eng.patches != patches {
		t.Fatalf("list placeholder err = %v patches=%d", err, eng.patches-patches)
	}
	// A new placeholder value is never written.
	_, err = env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "onvif", YAML: "password: \"*****\"\n"})
	if !errors.As(err, &ve) || eng.section("onvif")["password"] != realPassword {
		t.Fatalf("typed placeholder err = %v", err)
	}
}

func TestCameraYAMLSecretHolderEditsKeepCredentialsIntact(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	sec, err := env.Svc.GetCameraYAMLSection(ctx, env.Admin, cam.ID, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "ffmpeg", YAML: strings.Replace(sec.YAML, "- detect", "- detect\n      - record", 1)}); err != nil {
		t.Fatal(err)
	}
	inputs := eng.section("ffmpeg")["inputs"].([]any)
	in := inputs[0].(map[string]any)
	if in["path"] != realPath || len(in["roles"].([]any)) != 2 {
		t.Fatalf("inputs = %v", inputs)
	}
	row := configAudit(t, env, cam.ID)[0].Details
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("audit leaks the credentials: %s", raw)
	}
}

func setONVIF(t *testing.T, env *demofix.Env, cam string, endpoint string, caps string) {
	t.Helper()
	c := env.Cameras[cam]
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE cameras SET onvif_endpoint = $2, onvif_capabilities = $3::jsonb, onvif_version = '2.0', onvif_profile = 'ONVIF-T' WHERE id = $1`, c.ID, endpoint, caps)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCameraAdvancedPanelsRedactStreamURIs(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	const key = "frigate-h01/acceso_norte"
	cam := env.Cameras[key]
	setONVIF(t, env, key, "http://admin:hunter2@192.168.1.50/onvif/device_service", `{
	  "manufacturer":"Axis","model":"P3245","firmware_version":"10.1","serial_number":"SN123","has_ptz":true,"has_events":false,
	  "main_stream_url":"rtsp://admin:hunter2@192.168.1.50/main?token=abc","profiles":["Profile S","Profile T"],
	  "services":[{"namespace":"http://www.onvif.org/ver10/media/wsdl","xaddrs":["http://admin:hunter2@192.168.1.50/x"]}],
	  "media_profiles":[{"token":"p1","name":"main","encoding":"H264","width":1920,"height":1080,"fps":25,"bitrate":4096,
	    "stream_uri":"rtsp://admin:hunter2@192.168.1.50:554/main?token=abc#frag"}],
	  "supported_analytics":["tns1:RuleEngine/CellMotionDetector/Motion"]}`)

	adv, err := env.Svc.GetCameraAdvanced(context.Background(), env.Admin, cam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !adv.Frigate.Available || adv.Frigate.Version != "0.17.2" || len(adv.Frigate.Sections) == 0 {
		t.Fatalf("frigate = %+v", adv.Frigate)
	}
	o := adv.ONVIF
	if !o.Configured || o.Endpoint != "http://192.168.1.50/onvif/device_service" || o.Manufacturer != "Axis" || !o.Capabilities["has_ptz"] || len(o.Profiles) != 1 {
		t.Fatalf("onvif = %+v", o)
	}
	if o.Profiles[0].StreamURI != "rtsp://192.168.1.50:554/main" || o.Profiles[0].Width != 1920 {
		t.Fatalf("profile = %+v", o.Profiles[0])
	}
	raw, _ := json.Marshal(adv)
	for _, leak := range []string{"hunter2", "token=abc", "SN123", "admin:"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("advanced panels leak %q: %s", leak, raw)
		}
	}
	if adv.Provision.Evidence {
		t.Fatalf("Axis camera must not look like a Provision ISR: %+v", adv.Provision)
	}
}

func TestCameraConfigHistoryAndRollbackRestoreOldValues(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	r1, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()})
	if err != nil {
		t.Fatal(err)
	}
	rec := camconfig.Patch{Recording: &camconfig.RecordingPatch{Enabled: bptr(false)}}
	r2, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: rec, ExpectedRevision: r1.State.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}

	hist, err := env.Svc.ListCameraConfigHistory(ctx, env.Admin, cam.ID, 10)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history = %+v (%v)", hist, err)
	}
	if hist[0].Changes[0].Path != "recording.enabled" || hist[0].Changes[0].From != true || hist[0].Changes[0].To != false || !hist[0].Rollbackable || hist[0].Origin != "ui" || hist[0].UserName == "" {
		t.Fatalf("newest entry = %+v", hist[0])
	}

	if _, err := env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{ExpectedRevision: 0}, nil); !errors.Is(err, inventory.ErrRevisionConflict) {
		t.Fatalf("stale rollback err = %v", err)
	}
	patches := eng.patches

	// Default target: the latest change. The recording policy goes back to its old value.
	res, err := env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{ExpectedRevision: r2.State.DesiredRevision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.AuditID != hist[0].ID || res.Apply.Outcome.Result != camconfig.ResultOK || eng.section("record")["enabled"] != true {
		t.Fatalf("rollback = %+v record=%v", res, eng.section("record"))
	}
	if eng.patches != patches+1 {
		t.Fatalf("the rollback must go through one normal apply, patches %d -> %d", patches, eng.patches)
	}
	rows := configAudit(t, env, cam.ID)
	last := rows[len(rows)-1].Details
	if last["origin"] != "ui" || last["rollback_of"] != float64(hist[0].ID) {
		t.Fatalf("rollback audit = %+v", last)
	}
	after, _ := env.Svc.ListCameraConfigHistory(ctx, env.Admin, cam.ID, 10)
	if len(after) != 3 || after[0].RollbackOf != hist[0].ID || after[1].RolledBackBy != after[0].ID || after[1].Rollbackable {
		t.Fatalf("history after rollback = %+v", after)
	}

	// The next default target skips what was already undone and picks the detection change.
	res, err = env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{ExpectedRevision: res.Apply.State.DesiredRevision}, nil)
	if err != nil || eng.section("detect")["enabled"] != false {
		t.Fatalf("second rollback = %+v (%v) detect=%v", res, err, eng.section("detect"))
	}
	// Explicitly undoing a change that was already undone is refused.
	var ve *inventory.ValidationError
	_, err = env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{AuditID: hist[0].ID, ExpectedRevision: res.Apply.State.DesiredRevision}, nil)
	if !errors.As(err, &ve) {
		t.Fatalf("repeat rollback err = %v", err)
	}
	if _, err := env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{AuditID: 999999999, ExpectedRevision: res.Apply.State.DesiredRevision}, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown audit id err = %v", err)
	}
}

func TestCameraConfigRollbackRefusedWithoutOldValuesOrForYAML(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	delete(eng.doc, "record") // the engine did not report the recording policy: no old value
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	r1, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: camconfig.Patch{Recording: &camconfig.RecordingPatch{Enabled: bptr(true)}}})
	if err != nil {
		t.Fatal(err)
	}
	y, err := env.Svc.ApplyCameraYAML(ctx, env.Admin, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: "fps: 9\n", ExpectedRevision: r1.State.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	hist, _ := env.Svc.ListCameraConfigHistory(ctx, env.Admin, cam.ID, 10)
	if len(hist) != 2 || hist[0].Rollbackable || hist[1].Rollbackable || hist[0].RollbackReason == "" || hist[1].RollbackReason == "" {
		t.Fatalf("history = %+v", hist)
	}
	patches := eng.patches
	var ve *inventory.ValidationError
	for _, id := range []int64{0, hist[0].ID, hist[1].ID} {
		_, err := env.Svc.RollbackCameraConfig(ctx, env.Admin, cam.ID, inventory.RollbackInput{AuditID: id, ExpectedRevision: y.Revision}, nil)
		if !errors.As(err, &ve) {
			t.Errorf("audit %d: err = %v, want a validation error", id, err)
		}
	}
	if eng.patches != patches {
		t.Fatal("a refused rollback must not write")
	}
}

func TestCameraConfigApplyFailureCarriesPlainError(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	eng.patchErr = errors.New("ffmpeg exited with code 1 for rtsp://admin:hunter2@10.0.0.5/main?token=abc")
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()

	res, err := env.Svc.ApplyCameraConfig(ctx, env.Admin, cam.ID, inventory.ConfigApplyInput{Patch: detectionOn()})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Outcome.Plain
	if p.Message != "No se pudo iniciar el stream de la cámara." || strings.Contains(p.Message, "ffmpeg") {
		t.Fatalf("plain = %+v", p)
	}
	for _, leak := range []string{"hunter2", "token=abc"} {
		if strings.Contains(p.Technical, leak) || strings.Contains(res.State.LastError, leak) {
			t.Fatalf("leaks %q: %+v / %q", leak, p, res.State.LastError)
		}
	}
	if !strings.Contains(res.State.LastError, "ffmpeg exited with code 1") {
		t.Fatalf("last_error keeps the raw text for compatibility: %q", res.State.LastError)
	}
	hist, _ := env.Svc.ListCameraConfigHistory(ctx, env.Admin, cam.ID, 5)
	if len(hist) != 1 || hist[0].Result != "error" || hist[0].Error == nil || hist[0].Error.Message != p.Message || hist[0].Rollbackable {
		t.Fatalf("history = %+v", hist)
	}
}

// TestCameraYAMLAndAdvancedPermissionMatrix pins who reaches the YAML editor and the advanced
// panels: legacy holders keep access, nobody gains it, granular grants and denies are honored.
func TestCameraYAMLAndAdvancedPermissionMatrix(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	tenant := cam.TenantID

	yamlGet := func(u authz.Actor) error {
		_, err := env.Svc.GetCameraYAMLSection(ctx, u, cam.ID, "detect")
		return err
	}
	yamlValidate := func(u authz.Actor) error {
		_, err := env.Svc.ValidateCameraYAML(ctx, u, cam.ID, "detect", "enabled: true\n")
		return err
	}
	advanced := func(u authz.Actor) error {
		_, err := env.Svc.GetCameraAdvanced(ctx, u, cam.ID)
		return err
	}
	deny := func(u authz.Actor, p authz.Permission) {
		t.Helper()
		if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: u.UserID, Permission: p, Effect: authz.Deny, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name         string
		perms        []authz.Permission
		yaml, adv    bool
		denyYAML     bool
		denyAdvanced bool
	}{
		// Nobody gains access.
		{name: "no-grants", perms: nil},
		{name: "view-only", perms: []authz.Permission{authz.CamerasView}},
		{name: "manage-only", perms: []authz.Permission{authz.CamerasView, authz.CamerasManage}},
		{name: "secrets-only", perms: []authz.Permission{authz.CamerasView, authz.ServersConfigSecrets}},
		{name: "other-granular", perms: []authz.Permission{authz.CamerasView, authz.CamerasConfigEdit, authz.CamerasConfigVideo}},
		// Legacy holders keep what they had.
		{name: "legacy-servers-config", perms: []authz.Permission{authz.CamerasView, authz.ServersConfig}, adv: true},
		{name: "legacy-yaml-pair", perms: []authz.Permission{authz.CamerasView, authz.ServersConfig, authz.ServersConfigSecrets}, yaml: true, adv: true},
		// Granular grants.
		{name: "granular-yaml", perms: []authz.Permission{authz.CamerasView, authz.CamerasConfigYAML}, yaml: true},
		{name: "granular-advanced", perms: []authz.Permission{authz.CamerasView, authz.CamerasConfigAdvanced}, adv: true},
		// Legacy counts only while there is no granular allow (the U12 rule): revoking works.
		{name: "pair-plus-other-granular", perms: []authz.Permission{authz.CamerasView, authz.ServersConfig, authz.ServersConfigSecrets, authz.CamerasConfigEdit}},
		// An explicit deny wins over the legacy pair and over a granular allow.
		{name: "pair-denied-yaml", perms: []authz.Permission{authz.CamerasView, authz.ServersConfig, authz.ServersConfigSecrets}, adv: true, denyYAML: true},
		{name: "pair-denied-advanced", perms: []authz.Permission{authz.CamerasView, authz.ServersConfig, authz.ServersConfigSecrets}, yaml: true, denyAdvanced: true},
		{name: "granular-denied", perms: []authz.Permission{authz.CamerasView, authz.CamerasConfigYAML, authz.CamerasConfigAdvanced}, yaml: true, adv: true, denyYAML: true, denyAdvanced: true},
	}
	for _, c := range cases {
		u := grantAt(t, env, "mx-"+c.name, tenant, c.perms...)
		wantYAML, wantAdv := c.yaml && !c.denyYAML, c.adv && !c.denyAdvanced
		if c.denyYAML {
			deny(u, authz.CamerasConfigYAML)
		}
		if c.denyAdvanced {
			deny(u, authz.CamerasConfigAdvanced)
		}
		for name, call := range map[string]func(authz.Actor) error{"yaml get": yamlGet, "yaml validate": yamlValidate} {
			err := call(u)
			if wantYAML && err != nil {
				t.Errorf("%s: %s refused: %v", c.name, name, err)
			}
			if !wantYAML && !errors.Is(err, access.ErrForbidden) {
				t.Errorf("%s: %s err = %v, want forbidden", c.name, name, err)
			}
		}
		err := advanced(u)
		if wantAdv && err != nil {
			t.Errorf("%s: advanced refused: %v", c.name, err)
		}
		if !wantAdv && !errors.Is(err, access.ErrForbidden) {
			t.Errorf("%s: advanced err = %v, want forbidden", c.name, err)
		}
	}

	// A refused apply writes nothing.
	nothing := grantAt(t, env, "mx-apply-nothing", tenant, authz.CamerasView, authz.ServersConfig)
	if _, err := env.Svc.ApplyCameraYAML(ctx, nothing, cam.ID, inventory.YAMLApplyInput{Section: "detect", YAML: "fps: 9\n"}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("servers.config alone applied YAML: %v", err)
	}
	if eng.patches != 0 || len(configAudit(t, env, cam.ID)) != 0 {
		t.Fatalf("refused YAML apply wrote something: patches=%d", eng.patches)
	}

	// A grant in another tenant opens nothing here.
	other, err := env.Svc.CreateTenant(ctx, env.Admin, "otro-yaml", "Otro")
	if err != nil {
		t.Fatal(err)
	}
	foreign := grantAt(t, env, "mx-foreign", other.ID, authz.CamerasView, authz.CamerasConfigYAML, authz.CamerasConfigAdvanced)
	if err := yamlGet(foreign); err == nil {
		t.Error("a grant in another tenant opened the YAML")
	}
	if err := advanced(foreign); err == nil {
		t.Error("a grant in another tenant opened the advanced panels")
	}
}

// A holder of cameras.config.yaml without the secrets permission sees masked values, and saving
// the section keeps the real credentials: the placeholder round-trips.
func TestCameraYAMLGranularHolderWithoutSecretsKeepsRealCredentials(t *testing.T) {
	env := demofix.Setup(t)
	eng := newYAMLEngine()
	useYAMLEngine(env, eng)
	cam := env.Cameras["frigate-h01/acceso_norte"]
	ctx := context.Background()
	u := grantAt(t, env, "yaml-nosecrets", cam.TenantID, authz.CamerasView, authz.CamerasConfigYAML, authz.CamerasConfigVideo, authz.ServersConfig)

	sec, err := env.Svc.GetCameraYAMLSection(ctx, u, cam.ID, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if sec.SecretsVisible || strings.Contains(sec.YAML, "hunter2") || !strings.Contains(sec.YAML, maskedPath) {
		t.Fatalf("yaml = %q secrets=%v", sec.YAML, sec.SecretsVisible)
	}
	// Another key changes; the masked inputs stay as shown and are never sent.
	if _, err := env.Svc.ApplyCameraYAML(ctx, u, cam.ID, inventory.YAMLApplyInput{Section: "ffmpeg", YAML: strings.Replace(sec.YAML, "retry_interval: 10", "retry_interval: 20", 1)}); err != nil {
		t.Fatalf("saving with an untouched placeholder: %v", err)
	}
	ff := eng.section("ffmpeg")
	in := ff["inputs"].([]any)[0].(map[string]any)
	if ff["retry_interval"] != float64(20) || in["path"] != realPath {
		t.Fatalf("engine ffmpeg = %v", ff)
	}
	// Changing the inputs list means writing the stream URL: that needs the secrets permission.
	patches := eng.patches
	_, err = env.Svc.ApplyCameraYAML(ctx, u, cam.ID, inventory.YAMLApplyInput{Section: "ffmpeg", YAML: strings.Replace(sec.YAML, "- detect", "- detect\n      - record", 1)})
	if !errors.Is(err, access.ErrForbidden) || eng.patches != patches {
		t.Fatalf("inputs edit err = %v", err)
	}
	onvif, err := env.Svc.GetCameraYAMLSection(ctx, u, cam.ID, "onvif")
	if err != nil || strings.Contains(onvif.YAML, realPassword) {
		t.Fatalf("onvif yaml = %q (%v)", onvif.YAML, err)
	}
	if _, err := env.Svc.ApplyCameraYAML(ctx, u, cam.ID, inventory.YAMLApplyInput{Section: "onvif", YAML: strings.Replace(onvif.YAML, "host: 10.0.0.5", "host: 10.0.0.7", 1)}); err != nil {
		t.Fatal(err)
	}
	if on := eng.section("onvif"); on["host"] != "10.0.0.7" || on["password"] != realPassword || on["user"] != "admin" {
		t.Fatalf("engine onvif = %v", on)
	}
	// Writing a new credential still needs the secrets permission.
	_, err = env.Svc.ApplyCameraYAML(ctx, u, cam.ID, inventory.YAMLApplyInput{Section: "onvif", YAML: "password: another\n"})
	if !errors.Is(err, access.ErrForbidden) || eng.section("onvif")["password"] != realPassword {
		t.Fatalf("new credential err = %v", err)
	}
}
