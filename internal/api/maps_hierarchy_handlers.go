package api

import (
	"bytes"
	"context"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/store"
	"net/http"
)

func (h *Handlers) mapsEnabled() error {
	if !h.Features.Maps || h.Maps == nil {
		return store.ErrNotFound
	}
	return nil
}
func mapBuildingResponse(b *maps.Building) gen.MapBuilding {
	return gen.MapBuilding{Id: b.ID, SiteId: b.SiteID, Name: b.Name, Revision: &b.Revision, Floors: []gen.MapFloor{}}
}
func mapFloorResponse(f *maps.Floor) gen.MapFloor {
	return gen.MapFloor{Id: f.ID, BuildingId: f.BuildingID, Name: f.Name, Ordinal: f.Ordinal, Revision: &f.Revision, PlanKey: f.PlanKey, PlanContentType: f.PlanContentType, PlanWidthPx: f.PlanWidthPx, PlanHeightPx: f.PlanHeightPx}
}
func (h *Handlers) CreateMapBuilding(ctx context.Context, r gen.CreateMapBuildingRequestObject) (gen.CreateMapBuildingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &maps.ValidationError{Msg: "body is required"}
	}
	b, err := h.Maps.SaveBuilding(ctx, a, r.SiteId, nil, r.Body.Name, "")
	if err != nil {
		return nil, err
	}
	return gen.CreateMapBuilding201JSONResponse(mapBuildingResponse(b)), nil
}
func (h *Handlers) UpdateMapBuilding(ctx context.Context, r gen.UpdateMapBuildingRequestObject) (gen.UpdateMapBuildingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &maps.ValidationError{Msg: "body is required"}
	}
	b, err := h.Maps.SaveBuilding(ctx, a, r.SiteId, &r.BuildingId, r.Body.Name, r.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMapBuilding200JSONResponse(mapBuildingResponse(b)), nil
}
func (h *Handlers) DeleteMapBuilding(ctx context.Context, r gen.DeleteMapBuildingRequestObject) (gen.DeleteMapBuildingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if err = h.Maps.DeleteBuilding(ctx, a, r.SiteId, r.BuildingId, r.Params.IfMatch); err != nil {
		return nil, err
	}
	return gen.DeleteMapBuilding204Response{}, nil
}
func (h *Handlers) CreateMapFloor(ctx context.Context, r gen.CreateMapFloorRequestObject) (gen.CreateMapFloorResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &maps.ValidationError{Msg: "body is required"}
	}
	f, err := h.Maps.SaveFloor(ctx, a, r.SiteId, r.BuildingId, nil, r.Body.Name, r.Body.Ordinal, "")
	if err != nil {
		return nil, err
	}
	return gen.CreateMapFloor201JSONResponse(mapFloorResponse(f)), nil
}
func (h *Handlers) UpdateMapFloor(ctx context.Context, r gen.UpdateMapFloorRequestObject) (gen.UpdateMapFloorResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &maps.ValidationError{Msg: "body is required"}
	}
	f, err := h.Maps.SaveFloor(ctx, a, r.SiteId, [16]byte{}, &r.FloorId, r.Body.Name, r.Body.Ordinal, r.Params.IfMatch)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMapFloor200JSONResponse(mapFloorResponse(f)), nil
}
func (h *Handlers) DeleteMapFloor(ctx context.Context, r gen.DeleteMapFloorRequestObject) (gen.DeleteMapFloorResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	if err = h.Maps.DeleteFloor(ctx, a, r.SiteId, r.FloorId, r.Params.IfMatch); err != nil {
		return nil, err
	}
	return gen.DeleteMapFloor204Response{}, nil
}
func (h *Handlers) UploadMapFloorPlan(ctx context.Context, r gen.UploadMapFloorPlanRequestObject) (gen.UploadMapFloorPlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	f, err := h.Maps.UploadFloorPlan(ctx, a, r.SiteId, r.FloorId, r.Params.IfMatch, r.Body)
	if err != nil {
		return nil, err
	}
	return gen.UploadMapFloorPlan200JSONResponse(mapFloorResponse(f)), nil
}
func (h *Handlers) GetMapFloorPlan(ctx context.Context, r gen.GetMapFloorPlanRequestObject) (gen.GetMapFloorPlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.mapsEnabled(); err != nil {
		return nil, err
	}
	data, err := h.Maps.GetFloorPlan(ctx, a, r.SiteId, r.FloorId)
	if err != nil {
		return nil, err
	}
	return privateMapPlanResponse{gen.GetMapFloorPlan200ImagepngResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}}, nil
}

// Plans contain private site layouts; never retain images in shared/browser caches.
type privateMapPlanResponse struct {
	gen.GetMapFloorPlan200ImagepngResponse
}

func (r privateMapPlanResponse) VisitGetMapFloorPlanResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	return r.GetMapFloorPlan200ImagepngResponse.VisitGetMapFloorPlanResponse(w)
}
