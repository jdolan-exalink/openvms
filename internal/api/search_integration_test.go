//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/search"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type testSearchEnv struct {
	*demofix.Env
	server *httptest.Server
}

func setupSearchTest(t *testing.T) *testSearchEnv {
	t.Helper()
	env := demofix.Setup(t)
	searchSvc := &search.Service{
		Store: env.Store,
	}
	handlers := &api.Handlers{
		Inv:    env.Svc,
		Search: searchSvc,
		Log:    pgtest.Discard(),
	}
	router, err := api.NewRouter(handlers, pgtest.Discard(), api.Options{
		Queries: db.New(env.Pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return &testSearchEnv{
		Env:    env,
		server: ts,
	}
}

func (te *testSearchEnv) request(method, path, token string) (int, []byte) {
	req, _ := http.NewRequest(method, te.server.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestSearch_Validation(t *testing.T) {
	te := setupSearchTest(t)

	// Less than 2 characters -> 400
	code, body := te.request("GET", "/api/v1/search?q=a", te.AdminToken)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for q=a, got %d: %s", code, body)
	}

	code, body = te.request("GET", "/api/v1/search?q=", te.AdminToken)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty q, got %d: %s", code, body)
	}

	// Unauthenticated -> 401
	code, _ = te.request("GET", "/api/v1/search?q=test", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", code)
	}
}

func TestSearch_MatchingAndPermissions(t *testing.T) {
	te := setupSearchTest(t)

	camAllowed := te.Cameras["frigate-h01/acceso_norte"]
	camDenied := te.Cameras["frigate-h01/plaza"]
	ctx := context.Background()

	// Seed event on camAllowed with label "forklift"
	eventID := uuid.New()
	plateReadID := uuid.New()
	remoteEvID := "remote-ev-123"

	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, start_time, labels, detection_ids)
			VALUES ($1, $2, $3, $4, $5, $6, 'alert', now(), ARRAY['forklift']::text[], ARRAY[$6]::text[])
		`, eventID, camAllowed.TenantID, camAllowed.SiteID, camAllowed.ServerID, camAllowed.ID, remoteEvID)
		if err != nil {
			return err
		}

		// Seed LPR read on camAllowed with plate "XYZ-987"
		_, err = tx.Exec(ctx, `
			INSERT INTO lpr_reads (id, tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'XYZ-987', 'XYZ987', now())
		`, plateReadID, camAllowed.TenantID, camAllowed.SiteID, camAllowed.ServerID, camAllowed.ID, remoteEvID)
		if err != nil {
			return err
		}

		// Update tags on camDenied
		_, err = tx.Exec(ctx, `
			UPDATE cameras SET tags = ARRAY['restricted_zone'] WHERE id = $1
		`, camDenied.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Admin searches "forklift" -> finds event
	code, body := te.request("GET", "/api/v1/search?q=forklift", te.AdminToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	var res gen.SearchResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) == 0 {
		t.Fatalf("expected to find event for 'forklift', got 0")
	}
	if res.Events[0].Id != eventID {
		t.Errorf("expected event id %v, got %v", eventID, res.Events[0].Id)
	}

	// 2. Admin searches plate "XYZ" -> finds plate read linked to event
	code, body = te.request("GET", "/api/v1/search?q=XYZ", te.AdminToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	var plateRes gen.SearchResult
	if err := json.Unmarshal(body, &plateRes); err != nil {
		t.Fatal(err)
	}
	if len(plateRes.Plates) == 0 {
		t.Fatalf("expected to find plate for 'XYZ', got 0")
	}
	if plateRes.Plates[0].Plate != "XYZ-987" {
		t.Errorf("expected plate 'XYZ-987', got '%s'", plateRes.Plates[0].Plate)
	}
	if plateRes.Plates[0].EventId == nil || *plateRes.Plates[0].EventId != eventID {
		t.Errorf("expected plate linked to event %v, got %v", eventID, plateRes.Plates[0].EventId)
	}

	// 3. Operator searches "restricted_zone": operator does not have access to camDenied
	operadorToken := te.Demo.Tokens["operador"]
	code, body = te.request("GET", "/api/v1/search?q=restricted_zone", operadorToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	var opRes gen.SearchResult
	if err := json.Unmarshal(body, &opRes); err != nil {
		t.Fatal(err)
	}
	if len(opRes.Cameras) != 0 {
		t.Fatalf("operator should not see restricted camera, got %d cameras", len(opRes.Cameras))
	}

	// 4. Operator searches "acceso": operator has access to camAllowed
	code, body = te.request("GET", "/api/v1/search?q=acceso", operadorToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	var opAccesoRes gen.SearchResult
	if err := json.Unmarshal(body, &opAccesoRes); err != nil {
		t.Fatal(err)
	}
	if len(opAccesoRes.Cameras) == 0 {
		t.Fatalf("operator should see acceso camera, got 0")
	}
}
