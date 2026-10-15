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
	// OpenAlarm is true while a linked alarm is not resolved.
	OpenAlarm bool
	// PlatesAllowed is true when the actor holds lpr.view on the camera; plate text and scores
	// are never exposed otherwise.
	PlatesAllowed bool
}

// EventSource loads the recent events an actor may see.
type EventSource interface {
	RecentEvents(ctx context.Context, actor authz.Actor, window time.Duration, now time.Time) ([]EventRow, error)
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
	typ, severity := "detection", ""
	switch {
	case r.OpenAlarm:
		typ, severity = "alarm", "critical"
	case r.Severity == "alert":
		severity = "high"
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
  EXISTS (SELECT 1 FROM alarms a WHERE a.event_id = e.id AND a.status <> 'resolved'),
  e.camera_id = ANY($4)
FROM events e
JOIN cameras c ON c.id = e.camera_id
WHERE e.camera_id = ANY($1) AND c.deleted_at IS NULL AND c.enabled
  AND e.start_time > $2 AND (e.end_time IS NULL OR e.end_time > $3)
ORDER BY e.start_time DESC
LIMIT 500`

// RecentEvents returns events that are running or ended within window, on cameras the actor holds
// events.view for, newest first.
func (p PGSource) RecentEvents(ctx context.Context, actor authz.Actor, window time.Duration, now time.Time) ([]EventRow, error) {
	var out []EventRow
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
		rows, err := tx.Query(ctx, recentEventsSQL, cams, now.Add(-maxStartAge), now.Add(-window), lprCams)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r EventRow
			if err := rows.Scan(&r.ID, &r.TenantID, &r.SiteID, &r.ServerID, &r.CameraID, &r.CameraName,
				&r.Severity, &r.Labels, &r.Zones, &r.Plates, &r.StartTime, &r.EndTime,
				&r.LPRScore, &r.OpenAlarm, &r.PlatesAllowed); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
