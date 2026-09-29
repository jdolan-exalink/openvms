package rules

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
)

// OfflineDetector turns the inventory's health state into offline rule evaluations. It adds no
// probing of its own: the health poller already keeps frigate_servers.status/last_seen_at and
// cameras.status current, so a tick only reads them.
//
//   - A server is offline while status = 'offline'; the outage started at last_seen_at (its
//     created_at when it was never reachable).
//   - cameras.status has no "since", so the detector records the first tick that sees a camera
//     offline in camera_outages and drops the row once the camera recovers. The outage duration
//     is therefore accurate to one Interval. Cameras removed from Frigate (missing_since) and
//     disabled ones are not outages.
//
// Every tick re-evaluates every open outage; Service.EvaluateOffline notifies once per rule and
// outage, so repeating it is safe.
type OfflineDetector struct {
	Store    *store.Store
	Rules    *Service
	Log      *slog.Logger
	Interval time.Duration
}

type outage struct {
	tenantID uuid.UUID
	siteID   uuid.UUID
	id       uuid.UUID
	name     string
	duration time.Duration
}

// Run ticks until ctx is done.
func (d *OfflineDetector) Run(ctx context.Context) {
	interval := d.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		d.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick evaluates the offline rules for every camera and server currently down.
func (d *OfflineDetector) Tick(ctx context.Context) {
	cameras, servers, err := d.outages(ctx)
	if err != nil {
		d.Log.WarnContext(ctx, "read offline resources", "error", err)
		return
	}
	for _, o := range cameras {
		if err := d.Rules.EvaluateOffline(ctx, o.tenantID, TriggerCameraOffline, o.id, o.siteID, o.name, o.duration); err != nil {
			d.Log.WarnContext(ctx, "evaluate camera offline rules", "error", err, "camera_id", o.id)
		}
	}
	for _, o := range servers {
		if err := d.Rules.EvaluateOffline(ctx, o.tenantID, TriggerServerOffline, o.id, o.siteID, o.name, o.duration); err != nil {
			d.Log.WarnContext(ctx, "evaluate server offline rules", "error", err, "server_id", o.id)
		}
	}
}

// outages syncs camera_outages with cameras.status and lists the open camera and server outages.
func (d *OfflineDetector) outages(ctx context.Context) (cameras, servers []outage, err error) {
	err = d.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		const offlineCameras = `status = 'offline' AND enabled AND missing_since IS NULL AND deleted_at IS NULL`
		if _, err := tx.Exec(ctx, `
INSERT INTO camera_outages (camera_id, tenant_id) SELECT id, tenant_id FROM cameras WHERE `+offlineCameras+`
ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM camera_outages WHERE camera_id NOT IN (SELECT id FROM cameras WHERE `+offlineCameras+`)`); err != nil {
			return err
		}
		var err error
		if cameras, err = readOutages(ctx, tx, `
SELECT c.tenant_id, c.site_id, c.id, c.display_name, extract(epoch FROM now() - o.since)
FROM camera_outages o JOIN cameras c ON c.id = o.camera_id`); err != nil {
			return err
		}
		servers, err = readOutages(ctx, tx, `
SELECT tenant_id, site_id, id, name, extract(epoch FROM now() - coalesce(last_seen_at, created_at))
FROM frigate_servers WHERE status = 'offline' AND deleted_at IS NULL`)
		return err
	})
	return cameras, servers, err
}

func readOutages(ctx context.Context, tx pgx.Tx, query string) ([]outage, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []outage
	for rows.Next() {
		var o outage
		var secs float64
		if err := rows.Scan(&o.tenantID, &o.siteID, &o.id, &o.name, &secs); err != nil {
			return nil, err
		}
		o.duration = time.Duration(secs * float64(time.Second))
		out = append(out, o)
	}
	return out, rows.Err()
}
