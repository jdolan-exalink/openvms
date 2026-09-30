//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type testFrigateConfigEnv struct {
	*demofix.Env
	server *httptest.Server
}

func setupFrigateConfigTest(t *testing.T) *testFrigateConfigEnv {
	t.Helper()
	env := demofix.Setup(t)
	handlers := &api.Handlers{
		Inv: env.Svc,
		Log: pgtest.Discard(),
	}
	router, err := api.NewRouter(handlers, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return &testFrigateConfigEnv{
		Env:    env,
		server: ts,
	}
}

func (te *testFrigateConfigEnv) request(method, path, token string, body any) (int, []byte) {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	req, _ := http.NewRequest(method, te.server.URL+path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestCameraFrigateConfigAndRestart(t *testing.T) {
	te := setupFrigateConfigTest(t)
	ctx := context.Background()

	cam := te.Cameras["frigate-h01/acceso_norte"]
	srvID := cam.ServerID

	operatorToken := te.Demo.Tokens["operador"]

	t.Run("GetCameraFrigateConfig as Admin succeeds", func(t *testing.T) {
		status, raw := te.request(http.MethodGet, "/api/v1/cameras/"+cam.ID.String()+"/config", te.AdminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(raw))
		}
		var cfg gen.CameraFrigateConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.CameraName != "acceso_norte" || !cfg.DetectEnabled || !cfg.LprEnabled {
			t.Errorf("unexpected config: %+v", cfg)
		}
		if len(cfg.Zones) != 2 {
			t.Errorf("expected 2 zones, got %d", len(cfg.Zones))
		}
	})

	t.Run("UpdateCameraFrigateConfig forbidden without servers.config", func(t *testing.T) {
		f := false
		update := gen.CameraFrigateConfigUpdate{
			DetectEnabled: &f,
		}
		status, _ := te.request(http.MethodPatch, "/api/v1/cameras/"+cam.ID.String()+"/config", operatorToken, update)
		if status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", status)
		}
	})

	t.Run("UpdateCameraFrigateConfig succeeds as Admin and writes audit snapshot", func(t *testing.T) {
		f := false
		objects := []string{"car", "bus"}
		update := gen.CameraFrigateConfigUpdate{
			DetectEnabled:  &f,
			TrackedObjects: &objects,
		}
		status, raw := te.request(http.MethodPatch, "/api/v1/cameras/"+cam.ID.String()+"/config", te.AdminToken, update)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(raw))
		}
		var cfg gen.CameraFrigateConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.DetectEnabled {
			t.Errorf("expected detect_enabled to be false, got true")
		}
		if len(cfg.TrackedObjects) != 2 || cfg.TrackedObjects[0] != "bus" || cfg.TrackedObjects[1] != "car" {
			t.Errorf("expected [bus, car], got %v", cfg.TrackedObjects)
		}

		// Verify audit entry has before and after snapshots
		var auditCount int
		var detailsJSON []byte
		err := te.Pool.QueryRow(ctx,
			`SELECT count(*), coalesce(max(details::text), '') FROM audit_log WHERE action = $1 AND target_id = $2`,
			inventory.ActionServerConfigUpdated, srvID).Scan(&auditCount, &detailsJSON)
		if err != nil {
			t.Fatal(err)
		}
		if auditCount == 0 {
			t.Fatalf("expected SERVER_CONFIG_UPDATED audit entry")
		}
		var details map[string]any
		if err := json.Unmarshal(detailsJSON, &details); err != nil {
			t.Fatal(err)
		}
		if details["before"] == nil || details["after"] == nil {
			t.Fatalf("audit details must contain before and after snapshots: %s", string(detailsJSON))
		}
	})

	t.Run("RestartServer forbidden without servers.restart", func(t *testing.T) {
		status, _ := te.request(http.MethodPost, "/api/v1/servers/"+srvID.String()+"/restart", operatorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", status)
		}
	})

	t.Run("RestartServer succeeds as Admin and writes audit", func(t *testing.T) {
		status, raw := te.request(http.MethodPost, "/api/v1/servers/"+srvID.String()+"/restart", te.AdminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(raw))
		}
		var res map[string]any
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		if res["success"] != true {
			t.Fatalf("expected success: true, got %v", res)
		}

		// Verify audit entry
		var auditCount int
		err := te.Pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`,
			inventory.ActionServerRestarted, srvID).Scan(&auditCount)
		if err != nil {
			t.Fatal(err)
		}
		if auditCount == 0 {
			t.Fatalf("expected SERVER_RESTARTED audit entry")
		}
	})
}

func TestFrigateConfigEditor(t *testing.T) {
	te := setupFrigateConfigTest(t)
	ctx := context.Background()
	cam := te.Cameras["frigate-h01/acceso_norte"]
	srvID := cam.ServerID
	mock := te.Mocks["frigate-h01"].Server
	base := "/api/v1/cameras/" + cam.ID.String() + "/frigate-config"
	srvBase := "/api/v1/servers/" + srvID.String() + "/frigate-config"

	// The supervisor may edit this server's config but holds no secrets permission.
	sup := te.Actor(t, "supervisor")
	supToken := te.Demo.Tokens["supervisor"]
	if _, err := te.Svc.CreateGrant(ctx, te.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: sup.UserID, Permission: authz.ServersConfig, Effect: authz.Allow,
		ScopeType: authz.ScopeServer, ScopeID: &srvID,
	}); err != nil {
		t.Fatal(err)
	}

	patch := func(token string, sections map[string]any) (int, []byte) {
		return te.request(http.MethodPatch, base, token, map[string]any{"sections": sections})
	}
	decode := func(t *testing.T, raw []byte, out any) {
		t.Helper()
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
	}
	countRows := func(query string, args ...any) int {
		var n int
		if err := te.Pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// revision reads one revision (with YAML) through the API as admin; the table is under
	// row-level security, so a bare pool query would see nothing.
	revision := func(t *testing.T, id string) gen.FrigateConfigRevision {
		t.Helper()
		_, raw := te.request(http.MethodGet, srvBase+"/revisions?include_yaml=true&limit=200", te.AdminToken, nil)
		var list gen.FrigateConfigRevisionList
		decode(t, raw, &list)
		for _, r := range list.Items {
			if r.Id.String() == id {
				return r
			}
		}
		t.Fatalf("revision %s not found in %s", id, raw)
		return gen.FrigateConfigRevision{}
	}

	t.Run("reads mask secrets unless servers.config.secrets", func(t *testing.T) {
		status, raw := te.request(http.MethodGet, base, te.AdminToken, nil)
		var doc gen.FrigateCameraConfigDoc
		decode(t, raw, &doc)
		if status != http.StatusOK || !doc.Editable || !doc.SecretsVisible || !strings.Contains(string(raw), "s3cret") {
			t.Fatalf("admin: %d %s", status, raw)
		}
		status, raw = te.request(http.MethodGet, base, supToken, nil)
		decode(t, raw, &doc)
		if status != http.StatusOK || !doc.Editable || doc.SecretsVisible || strings.Contains(string(raw), "s3cret") || !strings.Contains(string(raw), "://*:*@") {
			t.Fatalf("supervisor: %d %s", status, raw)
		}
		if status, _ = te.request(http.MethodGet, base, te.Demo.Tokens["operador"], nil); status != http.StatusOK {
			t.Fatalf("operator reads with cameras.view, got %d", status)
		}
	})

	var liveRev string
	t.Run("patch applies live with the right update_topic and stores a revision", func(t *testing.T) {
		before := len(mock.SetCalls())
		status, raw := patch(te.AdminToken, map[string]any{"detect": map[string]any{"fps": 12}, "record": map[string]any{"enabled": false}})
		if status != http.StatusOK {
			t.Fatalf("%d %s", status, raw)
		}
		var res gen.FrigatePatchResult
		decode(t, raw, &res)
		if res.RestartRequired || len(res.Sections) != 2 || !res.Sections[0].AppliedLive || !res.Sections[1].AppliedLive {
			t.Fatalf("unexpected result: %s", raw)
		}
		calls := mock.SetCalls()[before:]
		if len(calls) != 2 || calls[0].UpdateTopic != "config/cameras/acceso_norte/detect" || calls[1].UpdateTopic != "config/cameras/acceso_norte/record" {
			t.Fatalf("calls: %+v", calls)
		}
		liveRev = res.RevisionId.String()
		rev := revision(t, liveRev)
		kind, beforeYAML, afterYAML := string(rev.Kind), *rev.BeforeYaml, *rev.AfterYaml
		if kind != "camera_patch" || beforeYAML == afterYAML || !strings.Contains(beforeYAML, "fps: 5") || !strings.Contains(afterYAML, "fps: 12") {
			t.Fatalf("revision kind=%s\nbefore=%s\nafter=%s", kind, beforeYAML, afterYAML)
		}
		if n := countRows(`SELECT count(*) FROM audit_log WHERE action = 'FRIGATE_CONFIG_PATCHED' AND details->>'revision_id' = $1`, liveRev); n != 1 {
			t.Fatalf("expected one FRIGATE_CONFIG_PATCHED audit row, got %d", n)
		}
	})

	t.Run("restart-required section is flagged", func(t *testing.T) {
		status, raw := patch(te.AdminToken, map[string]any{"onvif": map[string]any{"port": 8001}})
		var res gen.FrigatePatchResult
		decode(t, raw, &res)
		if status != http.StatusOK || !res.RestartRequired || res.Sections[0].AppliedLive || !res.Sections[0].RequiresRestart {
			t.Fatalf("%d %s", status, raw)
		}
		last := mock.SetCalls()
		if c := last[len(last)-1]; c.UpdateTopic != "" || c.RequiresRestart == nil || *c.RequiresRestart != 1 {
			t.Fatalf("restart section must not carry an update_topic: %+v", c)
		}
	})

	t.Run("redacted values are never written back", func(t *testing.T) {
		calls := len(mock.SetCalls())
		masked := map[string]any{"ffmpeg": map[string]any{"inputs": []any{map[string]any{"path": "rtsp://*:*@10.0.0.10:554/acceso_norte", "roles": []any{"detect"}}}}}
		if status, raw := patch(te.AdminToken, masked); status != http.StatusBadRequest {
			t.Fatalf("masked path: %d %s", status, raw)
		}
		if status, raw := patch(te.AdminToken, map[string]any{"onvif": map[string]any{"password": "********"}}); status != http.StatusBadRequest {
			t.Fatalf("masked password: %d %s", status, raw)
		}
		if len(mock.SetCalls()) != calls || !strings.Contains(mock.ConfigYAML(), "s3cret") || !strings.Contains(mock.ConfigYAML(), "onvifpass") {
			t.Fatal("nothing may reach Frigate and the real secrets must survive")
		}
	})

	t.Run("secrets permission is enforced", func(t *testing.T) {
		if status, _ := patch(supToken, map[string]any{"onvif": map[string]any{"password": "hunter2"}}); status != http.StatusForbidden {
			t.Fatalf("onvif password without secrets: %d", status)
		}
		ff := map[string]any{"ffmpeg": map[string]any{"inputs": []any{map[string]any{"path": "rtsp://u:p@10.0.0.10/x", "roles": []any{"detect"}}}}}
		if status, _ := patch(supToken, ff); status != http.StatusForbidden {
			t.Fatalf("ffmpeg inputs without secrets: %d", status)
		}
		if status, raw := patch(supToken, map[string]any{"detect": map[string]any{"fps": 7}}); status != http.StatusOK {
			t.Fatalf("plain section with servers.config: %d %s", status, raw)
		}
		if status, _ := te.request(http.MethodGet, srvBase+"/raw", supToken, nil); status != http.StatusForbidden {
			t.Fatalf("raw without secrets: %d", status)
		}
		if status, _ := te.request(http.MethodPut, srvBase+"/raw", supToken, map[string]any{"yaml": "cameras: {}"}); status != http.StatusForbidden {
			t.Fatalf("raw put without secrets: %d", status)
		}
		if status, _ := patch(te.Demo.Tokens["operador"], map[string]any{"detect": map[string]any{"fps": 7}}); status != http.StatusForbidden {
			t.Fatalf("operator has no servers.config: %d", status)
		}
		// With the permission the admin edits credentials; the new value lands in Frigate.
		if status, raw := patch(te.AdminToken, map[string]any{"onvif": map[string]any{"password": "rotated"}}); status != http.StatusOK {
			t.Fatalf("admin onvif password: %d %s", status, raw)
		}
		if !strings.Contains(mock.ConfigYAML(), "rotated") {
			t.Fatal("secret edit not applied")
		}
	})

	t.Run("revisions hide YAML and credentials without the secrets permission", func(t *testing.T) {
		status, raw := te.request(http.MethodGet, srvBase+"/revisions?include_yaml=true", supToken, nil)
		if status != http.StatusOK || strings.Contains(string(raw), "before_yaml") || strings.Contains(string(raw), "rotated") {
			t.Fatalf("supervisor: %d %s", status, raw)
		}
		status, raw = te.request(http.MethodGet, srvBase+"/revisions?include_yaml=true&camera_id="+cam.ID.String(), te.AdminToken, nil)
		var list gen.FrigateConfigRevisionList
		decode(t, raw, &list)
		if status != http.StatusOK || len(list.Items) < 3 || list.Items[0].BeforeYaml == nil || list.Items[0].AfterYaml == nil {
			t.Fatalf("admin: %d %s", status, raw)
		}
	})

	t.Run("rollback restores the previous config and needs a restart", func(t *testing.T) {
		beforeYAML := *revision(t, liveRev).BeforeYaml
		path := srvBase + "/revisions/" + liveRev + "/rollback"
		if status, _ := te.request(http.MethodPost, path, supToken, nil); status != http.StatusForbidden {
			t.Fatalf("rollback without secrets: %d", status)
		}
		status, raw := te.request(http.MethodPost, path, te.AdminToken, nil)
		var res gen.FrigateRawSaveResult
		decode(t, raw, &res)
		if status != http.StatusOK || !res.RestartRequired {
			t.Fatalf("%d %s", status, raw)
		}
		if mock.ConfigYAML() != beforeYAML {
			t.Fatalf("config not restored:\n%s", mock.ConfigYAML())
		}
		if rb := revision(t, res.RevisionId.String()); rb.Kind != gen.Rollback {
			t.Fatalf("rollback revision kind: %s", rb.Kind)
		}
		if n := countRows(`SELECT count(*) FROM audit_log WHERE action = 'FRIGATE_CONFIG_ROLLED_BACK' AND details->>'revision_id' = $1`, res.RevisionId.String()); n != 1 {
			t.Fatal("rollback audit row missing")
		}
	})

	t.Run("raw save validates through Frigate and stores a revision", func(t *testing.T) {
		status, raw := te.request(http.MethodPut, srvBase+"/raw", te.AdminToken, map[string]any{"yaml": "cameras:\n  acceso_norte:\n    ffmpeg:\n      inputs: []\n"})
		if status != http.StatusBadRequest || !strings.Contains(string(raw), "ffmpeg.inputs") {
			t.Fatalf("invalid YAML: %d %s", status, raw)
		}
		status, raw = te.request(http.MethodGet, srvBase+"/raw", te.AdminToken, nil)
		var cur gen.FrigateRawConfig
		decode(t, raw, &cur)
		if status != http.StatusOK || cur.Yaml == "" {
			t.Fatalf("raw read: %d", status)
		}
		status, raw = te.request(http.MethodPut, srvBase+"/raw?restart=true", te.AdminToken, cur)
		var res gen.FrigateRawSaveResult
		decode(t, raw, &res)
		if status != http.StatusOK || res.RestartRequired {
			t.Fatalf("%d %s", status, raw)
		}
		calls := mock.SaveCalls()
		if last := calls[len(calls)-1]; last.Option != "restart" {
			t.Fatalf("save option: %+v", last)
		}
		if n := countRows(`SELECT count(*) FROM audit_log WHERE action = 'FRIGATE_CONFIG_RAW_SAVED' AND details->>'revision_id' = $1`, res.RevisionId.String()); n != 1 {
			t.Fatal("raw save audit row missing")
		}
	})

	t.Run("schema is served trimmed", func(t *testing.T) {
		status, raw := te.request(http.MethodGet, srvBase+"/schema", supToken, nil)
		if status != http.StatusOK || !strings.Contains(string(raw), "CameraConfig") || strings.Contains(string(raw), "MqttConfig") {
			t.Fatalf("%d %s", status, raw)
		}
	})

	t.Run("Frigate older than 0.16 is read-only", func(t *testing.T) {
		mock.Version = "0.15.2"
		t.Cleanup(func() { mock.Version = "0.17.2-mock" })
		calls := len(mock.SetCalls())
		if status, _ := patch(te.AdminToken, map[string]any{"detect": map[string]any{"fps": 3}}); status != http.StatusConflict {
			t.Fatalf("patch on 0.15: %d", status)
		}
		if status, _ := te.request(http.MethodPut, srvBase+"/raw", te.AdminToken, map[string]any{"yaml": "cameras: {}"}); status != http.StatusConflict {
			t.Fatalf("raw put on 0.15: %d", status)
		}
		status, raw := te.request(http.MethodGet, base, te.AdminToken, nil)
		var doc gen.FrigateCameraConfigDoc
		decode(t, raw, &doc)
		if status != http.StatusOK || doc.Editable {
			t.Fatalf("0.15 read must work and not be editable: %d %s", status, raw)
		}
		if len(mock.SetCalls()) != calls {
			t.Fatal("nothing may reach Frigate")
		}
	})
}
