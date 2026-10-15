package visionai

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	// DefaultConfidence is used when an event has no LPR read to take a score from: the events
	// table has no per-detection confidence column.
	DefaultConfidence = 0.75

	// maxStartAge bounds how old a still-open event may be (Frigate leaves stale open rows).
	maxStartAge = time.Hour
)

// EventRow is one recent event as loaded for the actor, before it is split per label.
type EventRow struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	SiteID     uuid.UUID
	ServerID   uuid.UUID
	CameraID   uuid.UUID
	CameraName string
	Severity   string // "alert" | "detection"
	Labels     []string
	Zones      []string
	Plates     []string
	StartTime  time.Time
	EndTime    *time.Time
	// LPRScore is the best plate read score linked to the event, if any.
	LPRScore *float32
	// PlatesAllowed is true when the actor holds lpr.view on the camera; plate text and scores
	// are never exposed otherwise.
	PlatesAllowed bool
}

// EventSet is what an EventSource returns for one actor.
type EventSet struct {
	Rows []EventRow
	// PlatesVisible is true when the actor holds lpr.view on at least one of the cameras it can
	// see events for. It is independent of whether any row currently carries a plate.
	PlatesVisible bool
}

// EventSource loads the recent events an actor may see.
type EventSource interface {
	RecentEvents(ctx context.Context, actor authz.Actor, window time.Duration, now time.Time) (EventSet, error)
}

// anyShared reports whether the two camera lists have at least one camera in common.
func anyShared(eventCams, lprCams []uuid.UUID) bool {
	if len(eventCams) == 0 || len(lprCams) == 0 {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(lprCams))
	for _, id := range lprCams {
		seen[id] = struct{}{}
	}
	for _, id := range eventCams {
		if _, ok := seen[id]; ok {
			return true
		}
	}
	return false
}

// RowToEvents maps an event row to one VisionEvent per label.
func RowToEvents(r EventRow, cfg VisionConfig) []VisionEvent {
	plate := ""
	if r.PlatesAllowed && len(r.Plates) > 0 {
		plate = r.Plates[0]
	}
	confidence := DefaultConfidence
	if plate != "" && r.LPRScore != nil {
		confidence = float64(*r.LPRScore)
	}
	// Priority comes from Frigate's review severity. The alarms table is not a signal here: rules
	// create one per event and many deployments never close them, which would flag everything.
	typ, severity := "detection", ""
	if r.Severity == "alert" {
		typ, severity = "alarm", "high"
	}
	if typ == "detection" && plate != "" {
		typ = "lpr"
	}
	zone := pickZone(r.Zones, cfg.Zones)

	out := make([]VisionEvent, 0, len(r.Labels))
	for _, label := range r.Labels {
		out = append(out, VisionEvent{
			ID: r.ID.String(), TenantID: r.TenantID, SiteID: r.SiteID, ServerID: r.ServerID,
			CameraID: r.CameraID, CameraName: r.CameraName, Type: typ, Object: label,
			Confidence: confidence, Zone: zone, Timestamp: r.StartTime, StartTime: r.StartTime, EndTime: r.EndTime,
			AlarmSeverity: severity, Plate: plate,
			// Watchlist stays false: there is no watchlist data source yet.
		})
	}
	return out
}

func pickZone(zones, preferred []string) string {
	for _, z := range zones {
		for _, p := range preferred {
			if strings.EqualFold(z, p) {
				return z
			}
		}
	}
	if len(zones) > 0 {
		return zones[0]
	}
	return ""
}

// PGSource is the PostgreSQL-backed EventSource.
type PGSource struct{ Store *store.Store }

const recentEventsSQL = `SELECT e.id, e.tenant_id, e.site_id, e.server_id, e.camera_id, c.display_name,
  e.severity, e.labels, e.zones, e.plates, e.start_time, e.end_time,
  (SELECT max(r.score) FROM lpr_reads r
     WHERE r.server_id = e.server_id AND r.remote_event_id = ANY(e.detection_ids)),
  COALESCE(e.camera_id = ANY($4), false)
FROM events e
JOIN cameras c ON c.id = e.camera_id
WHERE e.camera_id = ANY($1) AND c.deleted_at IS NULL AND c.enabled
  AND e.start_time > $2 AND (e.end_time IS NULL OR e.end_time > $3)
ORDER BY e.start_time DESC
LIMIT 500`

// RecentEvents returns events that are running or ended within window, on cameras the actor holds
// events.view for, newest first. Still-open events older than maxStartAge (one hour) are dropped
// even though they are "running": Frigate leaves stale open rows behind.
func (p PGSource) RecentEvents(ctx context.Context, actor authz.Actor, window time.Duration, now time.Time) (EventSet, error) {
	var out EventSet
	err := p.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		c, err := access.Load(ctx, db.New(tx), actor)
		if err != nil {
			return err
		}
		cams, err := c.CameraIDs(ctx, authz.EventsView)
		if err != nil {
			return err
		}
		lprCams, err := c.CameraIDs(ctx, authz.LPRView)
		if err != nil {
			return err
		}
		out.PlatesVisible = anyShared(cams, lprCams)
		out.Rows, err = loadRows(ctx, tx, cams, lprCams, window, now)
		return err
	})
	return out, err
}

// loadRows runs the recent-events query. lprCams may be nil (no lpr.view anywhere): pgx sends a nil
// slice as NULL, so it is normalized to empty here and the comparison is also COALESCEd in SQL.
func loadRows(ctx context.Context, tx pgx.Tx, cams, lprCams []uuid.UUID, window time.Duration, now time.Time) ([]EventRow, error) {
	if lprCams == nil {
		lprCams = []uuid.UUID{}
	}
	rows, err := tx.Query(ctx, recentEventsSQL, cams, now.Add(-maxStartAge), now.Add(-window), lprCams)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.SiteID, &r.ServerID, &r.CameraID, &r.CameraName,
			&r.Severity, &r.Labels, &r.Zones, &r.Plates, &r.StartTime, &r.EndTime,
			&r.LPRScore, &r.PlatesAllowed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
