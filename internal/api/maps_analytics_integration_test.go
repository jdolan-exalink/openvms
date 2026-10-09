//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestMapsAnalyticsIntegration(t *testing.T) {
	te := setupMapsTest(t, true)
	ctx := context.Background()
	tenantID := te.Demo.TenantID

	cam := te.Cameras["frigate-h01/acceso_norte"]
	siteID := cam.SiteID

	now := time.Now().UTC().Truncate(time.Hour)
	eventTimeRecent := now.Add(-2 * time.Hour)
	olderEventTime := now.Add(-48 * time.Hour)

	// Seed placement for the camera and seed recent and older raw events
	err := te.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		// Update site geo
		_, err := tx.Exec(ctx, `
			UPDATE sites
			SET lat = -31.4201, lng = -64.1888, default_zoom = 16.5
			WHERE id = $1
		`, siteID)
		if err != nil {
			return err
		}

		// Place camera on geo map
		_, err = tx.Exec(ctx, `
			INSERT INTO map_placements (
				tenant_id, site_id, entity_type, entity_id, floor_id,
				lat, lng, bearing_deg, fov_deg, range_m, props, revision
			) VALUES (
				$1, $2, 'camera', $3, NULL,
				-31.4202, -64.1887, 90, 70, 40, '{}'::jsonb, 1
			)
			ON CONFLICT (entity_type, entity_id) WHERE floor_id IS NULL DO UPDATE
			SET lat = EXCLUDED.lat, lng = EXCLUDED.lng, bearing_deg = EXCLUDED.bearing_deg, range_m = EXCLUDED.range_m
		`, tenantID, siteID, cam.ID)
		if err != nil {
			return err
		}

		// Insert 3 raw events within last 2 hours
		for i := 0; i < 3; i++ {
			remID := fmt.Sprintf("rem-analytics-%d", i)
			_, err = tx.Exec(ctx, `
				INSERT INTO events (
					tenant_id, site_id, server_id, camera_id, remote_id,
					severity, labels, start_time, end_time
				) VALUES (
					$1, $2, $3, $4, $5,
					'detection', ARRAY['person']::text[], $6, $6
				)
			`, tenantID, siteID, cam.ServerID, cam.ID, remID, eventTimeRecent.Add(time.Duration(i)*time.Minute))
			if err != nil {
				return err
			}
		}

		// Insert 5 raw events 48h ago, outside the last-24h window
		for i := 0; i < 5; i++ {
			remID := fmt.Sprintf("rem-analytics-old-%d", i)
			_, err = tx.Exec(ctx, `
				INSERT INTO events (
					tenant_id, site_id, server_id, camera_id, remote_id,
					severity, labels, start_time, end_time
				) VALUES (
					$1, $2, $3, $4, $5,
					'detection', ARRAY['person']::text[], $6, $6
				)
			`, tenantID, siteID, cam.ServerID, cam.ID, remID, olderEventTime.Add(time.Duration(i)*time.Minute))
			if err != nil {
				return err
			}
		}
		return err
	})
	if err != nil {
		t.Fatalf("failed to seed map analytics data: %v", err)
	}

	t.Run("RecentEventsHeatmap", func(t *testing.T) {
		start := now.Add(-6 * time.Hour).Format(time.RFC3339)
		end := now.Add(1 * time.Hour).Format(time.RFC3339)
		path := fmt.Sprintf("/api/v1/maps/analytics?site_id=%s&metric=person&start=%s&end=%s", siteID, start, end)

		status, _, body := te.request(http.MethodGet, path, te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(body))
		}

		var res gen.MapAnalyticsResponse
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("failed to unmarshal analytics response: %v", err)
		}

		if res.Total != 3 {
			t.Fatalf("expected total 3 events, got %d", res.Total)
		}
		if res.MaxCount != 3 {
			t.Fatalf("expected max_count 3, got %d", res.MaxCount)
		}
		if len(res.Points) != 1 {
			t.Fatalf("expected 1 point without coverage, got %d", len(res.Points))
		}
		p := res.Points[0]
		if p.CameraId == nil || *p.CameraId != cam.ID {
			t.Fatalf("expected camera ID %s, got %v", cam.ID, p.CameraId)
		}
		if p.Weight != 1.0 {
			t.Fatalf("expected normalized weight 1.0, got %f", p.Weight)
		}
		if p.Count != 3 {
			t.Fatalf("expected count 3, got %d", p.Count)
		}
	})

	t.Run("CoverageSpreadsWeight", func(t *testing.T) {
		start := now.Add(-6 * time.Hour).Format(time.RFC3339)
		end := now.Add(1 * time.Hour).Format(time.RFC3339)
		path := fmt.Sprintf("/api/v1/maps/analytics?site_id=%s&metric=person&start=%s&end=%s&coverage=true", siteID, start, end)

		status, _, body := te.request(http.MethodGet, path, te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(body))
		}

		var res gen.MapAnalyticsResponse
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("failed to unmarshal analytics response: %v", err)
		}

		if len(res.Points) != 3 {
			t.Fatalf("expected 3 points with coverage=true, got %d", len(res.Points))
		}
		// Each point should have weight ~1.0 / 3 = 0.3333...
		expectedWeight := 1.0 / 3.0
		for idx, pt := range res.Points {
			if pt.Weight < expectedWeight-0.01 || pt.Weight > expectedWeight+0.01 {
				t.Fatalf("point %d expected weight ~%f, got %f", idx, expectedWeight, pt.Weight)
			}
		}
	})

	t.Run("LongRangeCountsRawEvents", func(t *testing.T) {
		start := now.Add(-72 * time.Hour).Format(time.RFC3339)
		end := now.Add(-24 * time.Hour).Format(time.RFC3339)
		path := fmt.Sprintf("/api/v1/maps/analytics?site_id=%s&metric=person&start=%s&end=%s", siteID, start, end)

		status, _, body := te.request(http.MethodGet, path, te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", status, string(body))
		}

		var res gen.MapAnalyticsResponse
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("failed to unmarshal analytics response: %v", err)
		}

		if res.Total != 5 {
			t.Fatalf("expected total 5 events, got %d", res.Total)
		}
		if res.MaxCount != 5 {
			t.Fatalf("expected max_count 5, got %d", res.MaxCount)
		}
	})

	t.Run("RBACFiltering", func(t *testing.T) {
		operadorActor := te.Actor(t, "operador")
		operatorToken := te.Demo.Tokens["operador"]

		// Operator without maps.view -> 403
		path := fmt.Sprintf("/api/v1/maps/analytics?site_id=%s", siteID)
		status, _, _ := te.request(http.MethodGet, path, operatorToken, nil, nil)
		if status != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden without maps.view, got %d", status)
		}

		// Grant maps.view to operator
		_, err := te.Env.Svc.CreateGrant(ctx, te.Env.Admin, inventory.GrantInput{
			SubjectType: "user",
			SubjectID:   operadorActor.UserID,
			Permission:  authz.MapsView,
			ScopeType:   authz.ScopeSite,
			ScopeID:     &siteID,
			Effect:      authz.Allow,
		})
		if err != nil {
			t.Fatalf("failed to grant maps.view: %v", err)
		}

		// Operator has maps.view AND events.view on acceso_norte -> gets analytics
		status, _, body := te.request(http.MethodGet, path, operatorToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200 OK after grant, got %d: %s", status, string(body))
		}
	})
}
