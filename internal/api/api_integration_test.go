//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestHTTPAuthorization(t *testing.T) {
	env := demofix.Setup(t)
	h := &api.Handlers{Inv: env.Svc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	do := func(t *testing.T, method, path, token, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), method, ts.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	operator := env.Demo.Tokens["operador"]
	cam := func(key string) string { return "/api/v1/cameras/" + env.Cameras[key].ID.String() }

	t.Run("no token is 401", func(t *testing.T) {
		if code, _ := do(t, "GET", "/api/v1/cameras", "", ""); code != http.StatusUnauthorized {
			t.Errorf("got %d", code)
		}
		if code, _ := do(t, "GET", "/api/v1/cameras", "ovms_invalid", ""); code != http.StatusUnauthorized {
			t.Errorf("bad token got %d", code)
		}
	})

	t.Run("system info stays public", func(t *testing.T) {
		if code, _ := do(t, "GET", "/api/v1/system/info", "", ""); code == http.StatusUnauthorized {
			t.Error("system info requires auth")
		}
	})

	t.Run("PRD §129 over HTTP", func(t *testing.T) {
		code, body := do(t, "GET", "/api/v1/cameras", operator, "")
		if code != http.StatusOK {
			t.Fatalf("list: %d %v", code, body)
		}
		var got []string
		for _, it := range body["items"].([]any) {
			got = append(got, it.(map[string]any)["remote_name"].(string))
		}
		slices.Sort(got)
		if !slices.Equal(got, []string{"acceso_norte", "muelle"}) {
			t.Errorf("operator list = %v", got)
		}
		for key, want := range map[string]int{
			"frigate-h01/acceso_norte": 200, "frigate-c01/muelle": 200,
			"frigate-h01/plaza": 403, "frigate-c01/plaza": 403,
		} {
			if code, _ := do(t, "GET", cam(key), operator, ""); code != want {
				t.Errorf("GET %s = %d, want %d", key, code, want)
			}
		}
	})

	t.Run("me lists the caller's grants", func(t *testing.T) {
		code, body := do(t, "GET", "/api/v1/me", operator, "")
		if code != http.StatusOK || body["username"] != "operador" {
			t.Errorf("me = %d %v", code, body)
		}
	})

	t.Run("other tenants get 404", func(t *testing.T) {
		code, body := do(t, "POST", "/api/v1/tenants", env.AdminToken, `{"slug":"otro","name":"Otro"}`)
		if code != http.StatusCreated {
			t.Fatalf("create tenant: %d %v", code, body)
		}
		tenantID := body["id"].(string)
		actor, otherToken, err := bootstrap.TenantUser(context.Background(), env.Store, uuid.MustParse(tenantID), "otro-op")
		if err != nil {
			t.Fatal(err)
		}
		code, body = do(t, "POST", "/api/v1/grants", env.AdminToken,
			`{"subject_type":"user","subject_id":"`+actor.UserID.String()+`","permission":"cameras.view","effect":"allow","scope_type":"tenant","scope_id":"`+tenantID+`"}`)
		if code != http.StatusCreated {
			t.Fatalf("grant: %d %v", code, body)
		}
		if code, _ := do(t, "GET", cam("frigate-h01/plaza"), otherToken, ""); code != http.StatusNotFound {
			t.Errorf("cross-tenant camera = %d, want 404", code)
		}
		if code, body := do(t, "GET", "/api/v1/cameras", otherToken, ""); code != 200 || len(body["items"].([]any)) != 0 {
			t.Errorf("cross-tenant list = %d %v", code, body)
		}
	})

	t.Run("operator cannot escalate", func(t *testing.T) {
		me := env.Actor(t, "operador")
		site := env.Cameras["frigate-h01/plaza"].SiteID.String()
		code, _ := do(t, "POST", "/api/v1/grants", operator,
			`{"subject_type":"user","subject_id":"`+me.UserID.String()+`","permission":"cameras.view","effect":"allow","scope_type":"site","scope_id":"`+site+`"}`)
		if code != http.StatusForbidden {
			t.Errorf("self-grant = %d, want 403", code)
		}
	})
}
