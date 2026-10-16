//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func TestDynamicCameraGroupsAPI(t *testing.T) {
	te, _ := setupRulesTest(t)
	ctx := context.Background()
	tenantID := te.Demo.TenantID
	manager, token, err := bootstrap.TenantUser(ctx, te.Store, tenantID, "grupos")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []authz.Permission{authz.CamerasManage, authz.CamerasView} {
		if _, err := te.Svc.CreateGrant(ctx, te.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: manager.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	cam := te.Cameras["frigate-h01/acceso_norte"]

	code, body := te.request("POST", "/api/v1/camera-groups", token, map[string]any{
		"tenant_id": tenantID, "name": "Acceso dinamico", "filter": map[string]any{"name_contains": "acceso"},
	})
	if code != 201 {
		t.Fatalf("create dynamic code = %d: %s", code, body)
	}
	var g gen.CameraGroup
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatal(err)
	}
	if g.Filter == nil || g.MemberCountTotal != 1 || g.MemberCountHidden != 0 || g.Version != 1 || len(g.CameraIds) != 1 || g.CameraIds[0] != cam.ID {
		t.Fatalf("created group = %+v", g)
	}
	path := "/api/v1/camera-groups/" + g.Id.String()
	put := map[string]any{"tenant_id": tenantID, "name": "Acceso dinamico", "filter": map[string]any{"tags": []string{"x"}}}

	if code, body := te.request("POST", "/api/v1/camera-groups", token, map[string]any{
		"tenant_id": tenantID, "name": "vacio", "filter": map[string]any{},
	}); code != 400 {
		t.Fatalf("empty filter code = %d, want 400: %s", code, body)
	}
	if code, body := te.request("PUT", path, token, map[string]any{"tenant_id": tenantID, "name": "x", "camera_ids": []string{cam.ID.String()}}); code != 400 {
		t.Fatalf("manual edit on dynamic code = %d, want 400: %s", code, body)
	}
	if code, body := te.requestH("PUT", path, token, put, map[string]string{"If-Match": "abc"}); code != 400 {
		t.Fatalf("garbage If-Match code = %d, want 400: %s", code, body)
	}
	if code, body := te.requestH("PUT", path, token, put, map[string]string{"If-Match": `"1"`}); code != 200 {
		t.Fatalf("update code = %d: %s", code, body)
	}
	if code, body := te.requestH("PUT", path, token, put, map[string]string{"If-Match": "1"}); code != 409 {
		t.Fatalf("stale If-Match code = %d, want 409: %s", code, body)
	}
}
