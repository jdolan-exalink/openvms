package api

import (
	"context"
	"encoding/json"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func (h *Handlers) ListCameraZones(ctx context.Context, r gen.ListCameraZonesRequestObject) (gen.ListCameraZonesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	zones, err := h.Inv.ListCameraZones(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	out := gen.CameraZoneList{Zones: make([]gen.CameraZone, 0, len(zones))}
	for _, zone := range zones {
		item, err := toCameraZone(zone)
		if err != nil {
			return nil, err
		}
		out.Zones = append(out.Zones, item)
	}
	return gen.ListCameraZones200JSONResponse(out), nil
}

func (h *Handlers) CreateCameraZone(ctx context.Context, r gen.CreateCameraZoneRequestObject) (gen.CreateCameraZoneResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	input := inventory.CameraZoneInput{}
	if r.Body != nil {
		input = cameraZoneInput(*r.Body)
	}
	zone, err := h.Inv.CreateCameraZone(ctx, a, r.CameraId, input)
	if err != nil {
		return nil, err
	}
	out, err := toCameraZone(zone)
	if err != nil {
		return nil, err
	}
	return gen.CreateCameraZone201JSONResponse(out), nil
}

func (h *Handlers) UpdateCameraZone(ctx context.Context, r gen.UpdateCameraZoneRequestObject) (gen.UpdateCameraZoneResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	input := inventory.CameraZoneInput{}
	if r.Body != nil {
		input = cameraZoneInput(*r.Body)
	}
	zone, err := h.Inv.UpdateCameraZone(ctx, a, r.CameraId, r.ZoneId, input)
	if err != nil {
		return nil, err
	}
	out, err := toCameraZone(zone)
	if err != nil {
		return nil, err
	}
	return gen.UpdateCameraZone200JSONResponse(out), nil
}

func (h *Handlers) DeleteCameraZone(ctx context.Context, r gen.DeleteCameraZoneRequestObject) (gen.DeleteCameraZoneResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteCameraZone(ctx, a, r.CameraId, r.ZoneId); err != nil {
		return nil, err
	}
	return gen.DeleteCameraZone204Response{}, nil
}

func (h *Handlers) RetryCameraZoneSync(ctx context.Context, r gen.RetryCameraZoneSyncRequestObject) (gen.RetryCameraZoneSyncResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.RetryCameraZoneSync(ctx, a, r.CameraId, r.ZoneId); err != nil {
		return nil, err
	}
	return gen.RetryCameraZoneSync202Response{}, nil
}

func cameraZoneInput(input gen.CameraZoneInput) inventory.CameraZoneInput {
	points := make([]inventory.CameraZonePoint, 0, len(input.Points))
	for _, p := range input.Points {
		points = append(points, inventory.CameraZonePoint{X: float64(p.X), Y: float64(p.Y)})
	}
	settings := json.RawMessage(`{}`)
	if input.Settings != nil {
		if raw, err := json.Marshal(*input.Settings); err == nil {
			settings = raw
		}
	}
	return inventory.CameraZoneInput{Name: input.Name, Points: points, Settings: settings}
}

func toCameraZone(zone inventory.CameraZone) (gen.CameraZone, error) {
	var settings map[string]interface{}
	if err := json.Unmarshal(zone.Settings, &settings); err != nil {
		return gen.CameraZone{}, err
	}
	if settings == nil {
		settings = map[string]interface{}{}
	}
	points := make([]gen.CameraZonePoint, 0, len(zone.Points))
	for _, p := range zone.Points {
		points = append(points, gen.CameraZonePoint{X: float32(p.X), Y: float32(p.Y)})
	}
	id, cameraID, createdAt, updatedAt := zone.ID, zone.CameraID, zone.CreatedAt, zone.UpdatedAt
	out := gen.CameraZone{
		Id: &id, CameraId: &cameraID, Name: zone.Name, Points: points, Settings: settings,
		CreatedAt: &createdAt, UpdatedAt: &updatedAt, SyncError: zone.SyncError,
	}
	if zone.SyncStatus != "" {
		status := gen.CameraZoneSyncStatus(zone.SyncStatus)
		out.SyncStatus = &status
	}
	return out, nil
}
