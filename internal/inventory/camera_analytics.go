package inventory

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// reasonServerClassifyOff is why vehicle classification cannot be switched for one camera.
const reasonServerClassifyOff = "Desactivado para todo el servidor"

// LocalAnalytics reads the per-camera switches of the analytics OpenVMS runs itself: a missing
// row is on, as the worker treats it. Vehicle classification keeps its own switches (server and
// camera, body_classify_*); color and clothing use camera_analytics_settings.
func (s *Service) LocalAnalytics(ctx context.Context, actor authz.Actor, cam db.GetCameraRow) (*camconfig.LocalAnalytics, error) {
	local := &camconfig.LocalAnalytics{
		Enabled: map[string]bool{
			camconfig.ModuleVehicleClassification: true,
			camconfig.ModuleVehicleColor:          true,
			camconfig.ModuleClothingColor:         true,
		},
		Locked: map[string]string{},
	}
	serverFlags, cameraFlags, err := loadClassifyFlags(ctx, s.Store, actor, []uuid.UUID{cam.ServerID}, []uuid.UUID{cam.ID})
	if err != nil {
		return nil, err
	}
	serverOn, cameraOn := true, true
	if v, ok := serverFlags[cam.ServerID]; ok {
		serverOn = v
	}
	if v, ok := cameraFlags[cam.ID]; ok {
		cameraOn = v
	}
	local.Enabled[camconfig.ModuleVehicleClassification] = serverOn && cameraOn
	if !serverOn {
		local.Locked[camconfig.ModuleVehicleClassification] = reasonServerClassifyOff
	}
	err = s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		rows, err := db.New(tx).ListCameraAnalyticsSettings(ctx, cam.ID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			local.Enabled[r.Module] = r.Enabled
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return local, nil
}

// LocalAnalyticsSource returns the loader a camconfig adapter uses to read the OpenVMS-side
// switches. They are not a reason to fail a read: when they cannot be loaded the modules simply
// read as unavailable.
func (s *Service) LocalAnalyticsSource(actor authz.Actor, cam db.GetCameraRow) func(context.Context) *camconfig.LocalAnalytics {
	return func(ctx context.Context) *camconfig.LocalAnalytics {
		local, err := s.LocalAnalytics(ctx, actor, cam)
		if err != nil {
			slog.WarnContext(ctx, "camera config: analytics switches not readable", "camera_id", cam.ID)
			return nil
		}
		return local
	}
}

// localWriter stores the OpenVMS-side module switches. The analytics section permission was
// already required by loadConfigTarget, so no extra permission is asked here.
type localWriter struct {
	s     *Service
	actor authz.Actor
	cam   db.GetCameraRow
}

func (w localWriter) SetModuleEnabled(ctx context.Context, module string, enabled bool) error {
	return w.s.Store.TxRaw(ctx, store.ScopeFor(w.actor), func(tx pgx.Tx) error {
		switch module {
		case camconfig.ModuleVehicleClassification:
			_, err := tx.Exec(ctx, `
				INSERT INTO body_classify_cameras (camera_id, tenant_id, enabled)
				VALUES ($1, $2, $3)
				ON CONFLICT (camera_id) DO UPDATE SET enabled = excluded.enabled`, w.cam.ID, w.cam.TenantID, enabled)
			return err
		case camconfig.ModuleVehicleColor, camconfig.ModuleClothingColor:
			return db.New(tx).UpsertCameraAnalyticsSetting(ctx, db.UpsertCameraAnalyticsSettingParams{
				CameraID: w.cam.ID, TenantID: w.cam.TenantID, Module: module, Enabled: enabled, UpdatedBy: &w.actor.UserID,
			})
		}
		return fmt.Errorf("camera analytics: %q is not stored by OpenVMS", module)
	})
}
