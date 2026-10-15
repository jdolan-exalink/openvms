//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
	"github.com/jdolan-exalink/openvms/internal/visionai"
)

func TestVisionAISlotsEndpoint(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	allowed := env.Cameras["frigate-h01/acceso_norte"]
	denied := env.Cameras["frigate-h01/plaza"]

	seed := func(cam db.GetCameraRow, remote, severity, sql string, args ...any) uuid.UUID {
		id := uuid.New()
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity,
				labels, zones, plates, detection_ids, start_time, end_time) VALUES ($1,$2,$3,$4,$5,$6,$7,`+sql, append([]any{id, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, remote, severity}, args...)...)
			return err
		})
		if err != nil {
			t.Fatalf("seed event: %v", err)
		}
		return id
	}
	// Running event with a plate, an LPR read and an open alarm, on the camera the operator can see.
	evAllowed := seed(allowed, "va-1", "alert",
		`ARRAY['car'], ARRAY['street'], ARRAY['AB123CD'], ARRAY['det-1'], now() - interval '20 seconds', NULL)`)
	// Running event on a camera the operator cannot see.
	seed(denied, "va-2", "detection",
		`ARRAY['person'], ARRAY[]::text[], ARRAY[]::text[], ARRAY[]::text[], now() - interval '10 seconds', NULL)`)
	// Ended long ago: outside the window.
	seed(env.Cameras["frigate-h01/plaza"], "va-3", "detection",
		`ARRAY['person'], ARRAY[]::text[], ARRAY[]::text[], ARRAY[]::text[], now() - interval '3 hours', now() - interval '2 hours')`)
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO lpr_reads (tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, score, seen_at)
			VALUES ($1,$2,$3,$4,'det-1','AB123CD','AB123CD',0.93,now())`, allowed.TenantID, allowed.SiteID, allowed.ServerID, allowed.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source, status) VALUES ($1,$2,$3,$4,'event','open')`,
			allowed.TenantID, allowed.SiteID, allowed.ID, evAllowed)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &api.Handlers{Inv: env.Svc, VisionAI: visionai.NewService(visionai.PGSource{Store: env.Store}), Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()
	get := func(token, query string) (int, gen.VisionAISlots) {
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/vision-ai/slots"+query, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out gen.VisionAISlots
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	cameras := func(s gen.VisionAISlots) map[uuid.UUID]gen.VisionAISlot {
		m := map[uuid.UUID]gen.VisionAISlot{}
		for _, sl := range s.Slots {
			if sl.CameraId != nil {
				m[*sl.CameraId] = sl
			}
		}
		return m
	}

	code, admin := get(env.AdminToken, "")
	if code != 200 || len(admin.Slots) != 4 {
		t.Fatalf("admin = %d with %d slots", code, len(admin.Slots))
	}
	am := cameras(admin)
	if len(am) != 2 {
		t.Fatalf("admin must see exactly the two running events, got %d cameras", len(am))
	}
	top := admin.Slots[0]
	if *top.CameraId != allowed.ID || deref(top.Alarm) != "critical" || deref(top.Plate) != "AB123CD" ||
		*top.Confidence < 0.92 || *top.Confidence > 0.94 || deref(top.Zone) != "street" {
		t.Fatalf("top slot should be the alarmed plate event: %+v", top)
	}

	code, op := get(env.Demo.Tokens["operador"], "?source=vehiculos&layout=9")
	if code != 200 || len(op.Slots) != 9 {
		t.Fatalf("operador = %d with %d slots", code, len(op.Slots))
	}
	if om := cameras(op); len(om) != 1 || om[allowed.ID].CameraId == nil {
		t.Fatalf("operador must only see the permitted camera: %v", om)
	}

	// operador holds events.view but not lpr.view: the plate event is listed without plate text.
	if ps := cameras(op)[allowed.ID]; deref(ps.Plate) != "" {
		t.Fatalf("actor without lpr.view must not see plates: %+v", ps)
	}

	if code, pe := get(env.AdminToken, "?source=personas"); code != 200 || len(cameras(pe)) != 1 {
		t.Fatalf("personas = %d, %d cameras", code, len(cameras(pe)))
	}
	if code, _ := get(env.AdminToken, "?source=nope"); code != 400 {
		t.Errorf("invalid source = %d, want 400", code)
	}
	if code, _ := get("", ""); code != 401 {
		t.Errorf("anonymous = %d, want 401", code)
	}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
