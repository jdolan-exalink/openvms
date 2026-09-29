package rules

import (
	"context"
	"log/slog"
	"time"

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
// The outage start lives in the database (camera_outages.since, frigate_servers.last_seen_at),
// so a worker restart resumes the same outages: rows are only deleted once the camera is back
// online, never on startup. Every tick re-evaluates every open outage; Service.EvaluateOffline
// compares the rule's last firing with that database-side outage start and notifies once per
// rule and outage, so repeating it, from this or a restarted worker, is safe.
type OfflineDetector struct {
	Store    *store.Store
	Rules    *Service
	Log      *slog.Logger
	Interval time.Duration
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
		if err := d.Rules.EvaluateOffline(ctx, TriggerCameraOffline, o); err != nil {
			d.Log.WarnContext(ctx, "evaluate camera offline rules", "error", err, "camera_id", o.ResourceID)
		}
	}
	for _, o := range servers {
		if err := d.Rules.EvaluateOffline(ctx, TriggerServerOffline, o); err != nil {
			d.Log.WarnContext(ctx, "evaluate server offline rules", "error", err, "server_id", o.ResourceID)
		}
	}
}

// outages syncs camera_outages with cameras.status and lists the open camera and server outages.
func (d *OfflineDetector) outages(ctx context.Context) (cameras, servers []Outage, err error) {
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
SELECT c.tenant_id, c.site_id, c.id, c.display_name, o.since, extract(epoch FROM now() - o.since)
FROM camera_outages o JOIN cameras c ON c.id = o.camera_id`); err != nil {
			return err
		}
		servers, err = readOutages(ctx, tx, `
SELECT tenant_id, site_id, id, name, coalesce(last_seen_at, created_at), extract(epoch FROM now() - coalesce(last_seen_at, created_at))
FROM frigate_servers WHERE status = 'offline' AND deleted_at IS NULL`)
		return err
	})
	return cameras, servers, err
}

func readOutages(ctx context.Context, tx pgx.Tx, query string) ([]Outage, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outage
	for rows.Next() {
		var o Outage
		var secs float64
		if err := rows.Scan(&o.TenantID, &o.SiteID, &o.ResourceID, &o.Name, &o.Since, &secs); err != nil {
			return nil, err
		}
		o.Duration = time.Duration(secs * float64(time.Second))
		out = append(out, o)
	}
	return out, rows.Err()
}
