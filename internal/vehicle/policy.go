package vehicle

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
)

// Effective is on unless the server or the camera was turned off.
// A nil flag is the default, which is on.
func Effective(server, camera *bool) bool {
	on := true
	if server != nil {
		on = *server
	}
	if camera != nil {
		on = on && *camera
	}
	return on
}

// BodyEnabled reports whether the crop classifier should run for this capture.
// A missing table (before the migration) stays on, matching the default.
func BodyEnabled(ctx context.Context, st *store.Store, serverID, cameraID uuid.UUID) (bool, error) {
	var serverOn, cameraOn *bool
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		var s, c bool
		err := tx.QueryRow(ctx, `SELECT enabled FROM body_classify_servers WHERE server_id = $1`, serverID).Scan(&s)
		if err == nil {
			serverOn = &s
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT enabled FROM body_classify_cameras WHERE camera_id = $1`, cameraID).Scan(&c)
		if err == nil {
			cameraOn = &c
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		if isMissingRelation(err) {
			return true, nil
		}
		return true, err
	}
	return Effective(serverOn, cameraOn), nil
}

// Per-camera analytics modules the worker computes itself (camera_analytics_settings.module).
const (
	ModuleVehicleColor  = "vehicle_color"
	ModuleClothingColor = "clothing_color"
)

// ModuleEnabled reports whether a per-camera module should run. A missing row, or a missing
// table (before the migration), is on: the worker computed these for every camera before there
// was a switch.
func ModuleEnabled(ctx context.Context, st *store.Store, cameraID uuid.UUID, module string) (bool, error) {
	on := true
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT enabled FROM camera_analytics_settings WHERE camera_id = $1 AND module = $2`, cameraID, module).Scan(&on)
		if errors.Is(err, pgx.ErrNoRows) {
			on = true
			return nil
		}
		return err
	})
	if err != nil {
		if isMissingRelation(err) {
			return true, nil
		}
		return true, err
	}
	return on, nil
}

// moduleOn is ModuleEnabled with the failure policy of the worker: a lookup error keeps the
// previous behavior (on) and is logged.
func (w *Worker) moduleOn(ctx context.Context, cameraID uuid.UUID, module string) bool {
	on, err := ModuleEnabled(ctx, w.Store, cameraID, module)
	if err != nil && w.Log != nil {
		w.Log.WarnContext(ctx, "analytics module switch", "module", module, "error", err)
	}
	return on
}
