package inventory

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var cameraZoneNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// CameraZonePoint is one point in Frigate's normalized 0..1 camera-frame coordinate space.
type CameraZonePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CameraZone is the VMS-owned persisted representation of a Frigate camera zone.
type CameraZone struct {
	ID        uuid.UUID         `json:"id"`
	CameraID  uuid.UUID         `json:"camera_id"`
	Name      string            `json:"name"`
	Points    []CameraZonePoint `json:"points"`
	Settings  json.RawMessage   `json:"settings"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// CameraZoneInput is the editable camera-zone payload. ID is assigned on create and retained on
// updates; the payload intentionally contains no tenant identifier.
type CameraZoneInput struct {
	Name     string            `json:"name"`
	Points   []CameraZonePoint `json:"points"`
	Settings json.RawMessage   `json:"settings,omitempty"`
}

type normalizedCameraZone struct {
	Name   string
	Points []CameraZonePoint
}

func normalizeCameraZone(name string, points []CameraZonePoint) (normalizedCameraZone, error) {
	name = strings.TrimSpace(name)
	if len(name) > 128 || !cameraZoneNamePattern.MatchString(name) {
		return normalizedCameraZone{}, invalid("zone name must be at most 128 lowercase letters, digits, or underscores")
	}
	if len(points) < 3 {
		return normalizedCameraZone{}, invalid("zone requires at least three points")
	}
	normalized := make([]CameraZonePoint, len(points))
	for i, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1 {
			return normalizedCameraZone{}, invalid("zone points must be normalized to the 0..1 camera frame")
		}
		normalized[i] = CameraZonePoint{X: math.Round(p.X*1000) / 1000, Y: math.Round(p.Y*1000) / 1000}
	}
	if polygonArea(normalized) <= 0 {
		return normalizedCameraZone{}, invalid("zone polygon must have non-zero area after normalization")
	}
	return normalizedCameraZone{Name: name, Points: normalized}, nil
}

func polygonArea(points []CameraZonePoint) float64 {
	area := 0.0
	for i, p := range points {
		next := points[(i+1)%len(points)]
		area += p.X*next.Y - next.X*p.Y
	}
	return math.Abs(area) / 2
}

func (s *Service) ListCameraZones(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) ([]CameraZone, error) {
	var zones []CameraZone
	err := s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		rows, err := q.ListCameraZonesByCamera(ctx, db.ListCameraZonesByCameraParams{TenantID: cam.TenantID, CameraID: cam.ID})
		if err != nil {
			return err
		}
		zones = make([]CameraZone, 0, len(rows))
		for _, row := range rows {
			zone, err := cameraZoneFromRow(row.ID, row.CameraID, row.Name, row.Points, row.Settings, row.CreatedAt, row.UpdatedAt)
			if err != nil {
				return err
			}
			zones = append(zones, zone)
		}
		return nil
	})
	return zones, err
}

func (s *Service) CreateCameraZone(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, input CameraZoneInput) (CameraZone, error) {
	zone, err := normalizeCameraZone(input.Name, input.Points)
	if err != nil {
		return CameraZone{}, err
	}
	settings, err := normalizeZoneSettings(input.Settings)
	if err != nil {
		return CameraZone{}, err
	}
	var out CameraZone
	err = s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		row, err := q.CreateCameraZone(ctx, db.CreateCameraZoneParams{TenantID: cam.TenantID, CameraID: cam.ID, Name: zone.Name, Points: mustJSON(zone.Points), Settings: settings})
		if err != nil {
			return store.Classify(err)
		}
		if err := enqueueCameraZone(ctx, q, actor, row.ID, cam.TenantID, cam.ID, zone.Name, zone.Points, settings, 1, nil, nil, "upsert"); err != nil {
			return err
		}
		out, err = cameraZoneFromRow(row.ID, row.CameraID, row.Name, row.Points, row.Settings, row.CreatedAt, row.UpdatedAt)
		return err
	})
	return out, err
}

func (s *Service) UpdateCameraZone(ctx context.Context, actor authz.Actor, cameraID, zoneID uuid.UUID, input CameraZoneInput) (CameraZone, error) {
	zone, err := normalizeCameraZone(input.Name, input.Points)
	if err != nil {
		return CameraZone{}, err
	}
	settings, err := normalizeZoneSettings(input.Settings)
	if err != nil {
		return CameraZone{}, err
	}
	var out CameraZone
	err = s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		if err := q.LockCameraZoneSync(ctx, zoneID.String()); err != nil {
			return err
		}
		oldName, err := q.GetCameraZoneName(ctx, db.GetCameraZoneNameParams{TenantID: cam.TenantID, ZoneID: zoneID})
		if err != nil {
			return notFoundOr(err)
		}
		row, err := q.UpdateCameraZone(ctx, db.UpdateCameraZoneParams{TenantID: cam.TenantID, CameraID: cam.ID, ID: zoneID, Name: zone.Name, Points: mustJSON(zone.Points), Settings: settings})
		if err != nil {
			return notFoundOr(err)
		}
		if err := q.SetCameraZoneDesiredSync(ctx, db.SetCameraZoneDesiredSyncParams{ZoneID: zoneID, TenantID: cam.TenantID}); err != nil {
			return err
		}
		revision, err := q.GetCameraZoneSyncRevision(ctx, db.GetCameraZoneSyncRevisionParams{ZoneID: zoneID, TenantID: cam.TenantID})
		if err != nil {
			return err
		}
		var previousName *string
		if oldName != zone.Name {
			previousName = &oldName
		}
		if err := enqueueCameraZone(ctx, q, actor, row.ID, cam.TenantID, cam.ID, zone.Name, zone.Points, settings, revision, nil, previousName, "upsert"); err != nil {
			return err
		}
		out, err = cameraZoneFromRow(row.ID, row.CameraID, row.Name, row.Points, row.Settings, row.CreatedAt, row.UpdatedAt)
		return err
	})
	return out, err
}

func enqueueCameraZone(ctx context.Context, q *db.Queries, actor authz.Actor, id, tenantID, cameraID uuid.UUID, name string, points []CameraZonePoint, settings json.RawMessage, revision int64, lastApplied []byte, previousName *string, operation string) error {
	if lastApplied == nil {
		stored, err := q.GetCameraZoneLastApplied(ctx, db.GetCameraZoneLastAppliedParams{ZoneID: id, TenantID: tenantID})
		if err != nil {
			return err
		}
		lastApplied = stored
	}
	var config map[string]any
	if err := json.Unmarshal(settings, &config); err != nil {
		return err
	}
	// Coordinates are converted to Frigate's conventional pixel string by the worker after
	// reading the camera's detect dimensions; the durable desired snapshot stores normalized points.
	config["_ovms_points"] = points
	config["_ovms_name"] = name
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = q.EnqueueCameraZoneSync(ctx, db.EnqueueCameraZoneSyncParams{ZoneID: id, TenantID: tenantID, CameraID: cameraID, ActorUserID: &actor.UserID, Operation: operation, DesiredRevision: revision, DesiredConfig: data, LastApplied: lastApplied, PreviousRemoteName: previousName})
	return err
}

func (s *Service) DeleteCameraZone(ctx context.Context, actor authz.Actor, cameraID, zoneID uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		if err := q.LockCameraZoneSync(ctx, zoneID.String()); err != nil {
			return err
		}
		zone, err := q.DeleteCameraZoneForSync(ctx, db.DeleteCameraZoneForSyncParams{ID: zoneID, TenantID: cam.TenantID, CameraID: cam.ID})
		if err != nil {
			return notFoundOr(err)
		}
		var last []byte
		if len(zone.LastApplied) > 0 {
			last = zone.LastApplied
		}
		if err := enqueueCameraZone(ctx, q, actor, zone.ID, zone.TenantID, zone.CameraID, zone.Name, nil, zone.Settings, zone.SyncRevision, last, nil, "delete"); err != nil {
			return err
		}
		return nil
	})
}

func normalizeZoneSettings(settings json.RawMessage) (json.RawMessage, error) {
	if len(settings) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(settings, &object); err != nil || object == nil {
		return nil, invalid("zone settings must be a JSON object")
	}
	if _, exists := object["name"]; exists {
		return nil, invalid("zone settings cannot override the zone name")
	}
	if _, exists := object["coordinates"]; exists {
		return nil, invalid("zone settings cannot override normalized points")
	}
	return json.Marshal(object)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func cameraZoneFromRow(id, cameraID uuid.UUID, name string, pointsJSON, settings json.RawMessage, createdAt, updatedAt time.Time) (CameraZone, error) {
	var points []CameraZonePoint
	if err := json.Unmarshal(pointsJSON, &points); err != nil {
		return CameraZone{}, err
	}
	return CameraZone{ID: id, CameraID: cameraID, Name: name, Points: points, Settings: settings, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}
