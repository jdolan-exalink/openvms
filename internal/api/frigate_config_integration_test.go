//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
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
