package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/maps"
)

// GetMapConfig reports the map configuration (tile provider, attribution, offline flag).
func (h *Handlers) GetMapConfig(ctx context.Context, _ gen.GetMapConfigRequestObject) (gen.GetMapConfigResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.GetMapConfig404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	cfg, err := h.Maps.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	var tiles *[]string
	if len(cfg.Provider.Tiles) > 0 {
		tiles = &cfg.Provider.Tiles
	}
	return gen.GetMapConfig200JSONResponse{
		Provider: gen.MapProviderConfig{
			Id:            cfg.Provider.ID,
			Kind:          gen.MapProviderConfigKind(cfg.Provider.Kind),
			StyleUrlLight: cfg.Provider.StyleURLLight,
			StyleUrlDark:  cfg.Provider.StyleURLDark,
			Tiles:         tiles,
			Attribution:   cfg.Provider.Attribution,
			MaxZoom:       cfg.Provider.MaxZoom,
			Offline:       cfg.Provider.Offline,
		},
		DefaultCenter: gen.MapDefaultCenter{
			Lat: cfg.DefaultCenter.Lat,
			Lng: cfg.DefaultCenter.Lng,
		},
		DefaultZoom: cfg.DefaultZoom,
	}, nil
}

// GetMapOverview returns a list of sites with geo positions and health/alarm aggregates.
func (h *Handlers) GetMapOverview(ctx context.Context, _ gen.GetMapOverviewRequestObject) (gen.GetMapOverviewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.GetMapOverview404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	overviews, err := h.Maps.GetOverview(ctx, a)
	if err != nil {
		return nil, err
	}
	items := make([]gen.MapSiteOverview, 0, len(overviews))
	for _, o := range overviews {
		items = append(items, toMapSiteOverview(o))
	}
	return gen.GetMapOverview200JSONResponse{
		Items: items,
	}, nil
}

// GetMapSite returns structural map data (bounds, buildings, floors, zones) for a site.
func (h *Handlers) GetMapSite(ctx context.Context, req gen.GetMapSiteRequestObject) (gen.GetMapSiteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.GetMapSite404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	details, err := h.Maps.GetSiteDetails(ctx, a, req.SiteId)
	if err != nil {
		return nil, err
	}
	return gen.GetMapSite200JSONResponse(toMapSiteDetails(details)), nil
}

// GetMapSiteEntities returns compact placed entities (cameras, servers, devices) with ETag support.
func (h *Handlers) GetMapSiteEntities(ctx context.Context, req gen.GetMapSiteEntitiesRequestObject) (gen.GetMapSiteEntitiesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.GetMapSiteEntities404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	f := maps.EntitiesFilter{
		FloorID: req.Params.FloorId,
		IsGeo:   req.Params.IsGeo,
		MinLat:  req.Params.MinLat,
		MinLng:  req.Params.MinLng,
		MaxLat:  req.Params.MaxLat,
		MaxLng:  req.Params.MaxLng,
	}
	res, err := h.Maps.GetSiteEntities(ctx, a, req.SiteId, f)
	if err != nil {
		return nil, err
	}

	etag := fmt.Sprintf(`"%d"`, res.Revision)
	if req.Params.IfNoneMatch != nil {
		reqETag := strings.TrimSpace(*req.Params.IfNoneMatch)
		if reqETag == etag || reqETag == fmt.Sprintf("%d", res.Revision) || reqETag == fmt.Sprintf(`W/"%d"`, res.Revision) {
			return gen.GetMapSiteEntities304Response{
				Headers: gen.GetMapSiteEntities304ResponseHeaders{
					ETag: &etag,
				},
			}, nil
		}
	}

	entities := make([]gen.MapEntity, 0, len(res.Entities))
	for _, e := range res.Entities {
		entities = append(entities, toMapEntity(e))
	}

	return gen.GetMapSiteEntities200JSONResponse{
		Headers: gen.GetMapSiteEntities200ResponseHeaders{
			ETag: &etag,
		},
		Body: gen.MapEntitiesResponse{
			Revision: res.Revision,
			Entities: entities,
		},
	}, nil
}

// GetMapUnplacedCameras returns cameras on the site that have no geographic placement.
func (h *Handlers) GetMapUnplacedCameras(ctx context.Context, req gen.GetMapUnplacedCamerasRequestObject) (gen.GetMapUnplacedCamerasResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.GetMapUnplacedCameras404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	cams, err := h.Maps.GetUnplacedCameras(ctx, a, req.Params.SiteId)
	if err != nil {
		return nil, err
	}
	items := make([]gen.MapUnplacedCamera, 0, len(cams))
	for _, c := range cams {
		items = append(items, gen.MapUnplacedCamera{
			Id:         c.ID,
			Name:       c.Name,
			RemoteName: c.RemoteName,
			SiteId:     c.SiteID,
			Status:     c.Status,
		})
	}
	return gen.GetMapUnplacedCameras200JSONResponse{
		SiteId:  req.Params.SiteId,
		Cameras: items,
	}, nil
}

// UpsertMapPlacement creates or updates a placement for a camera, server, or device.
func (h *Handlers) UpsertMapPlacement(ctx context.Context, req gen.UpsertMapPlacementRequestObject) (gen.UpsertMapPlacementResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.UpsertMapPlacement404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	if req.Body == nil {
		return nil, &maps.ValidationError{Msg: "request body is required"}
	}

	var rawProps json.RawMessage
	if req.Body.Props != nil {
		if b, err := json.Marshal(req.Body.Props); err == nil {
			rawProps = b
		}
	}

	p, err := h.Maps.UpsertPlacement(ctx, a, string(req.EntityType), req.EntityId, req.Params.IfMatch, maps.UpsertPlacementRequest{
		SiteID:     req.Body.SiteId,
		FloorID:    req.Body.FloorId,
		Lat:        req.Body.Lat,
		Lng:        req.Body.Lng,
		X:          req.Body.X,
		Y:          req.Body.Y,
		BearingDeg: req.Body.BearingDeg,
		FovDeg:     req.Body.FovDeg,
		RangeM:     req.Body.RangeM,
		Props:      rawProps,
	})
	if err != nil {
		return nil, err
	}

	etag := fmt.Sprintf(`"%d"`, p.Revision)
	return gen.UpsertMapPlacement200JSONResponse{
		Headers: gen.UpsertMapPlacement200ResponseHeaders{
			ETag: &etag,
		},
		Body: toMapPlacementResponse(p),
	}, nil
}

// DeleteMapPlacement removes a placement by placement ID.
func (h *Handlers) DeleteMapPlacement(ctx context.Context, req gen.DeleteMapPlacementRequestObject) (gen.DeleteMapPlacementResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.DeleteMapPlacement404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	if err := h.Maps.DeletePlacement(ctx, a, req.PlacementId, req.Params.IfMatch); err != nil {
		return nil, err
	}
	return gen.DeleteMapPlacement204Response{}, nil
}

// UpdateSiteGeo updates site geographic coordinates, zoom, and region.
func (h *Handlers) UpdateSiteGeo(ctx context.Context, req gen.UpdateSiteGeoRequestObject) (gen.UpdateSiteGeoResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !h.Features.Maps || h.Maps == nil {
		return gen.UpdateSiteGeo404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "not_found", Message: "maps feature is disabled"}}, nil
	}
	if req.Body == nil {
		return nil, &maps.ValidationError{Msg: "request body is required"}
	}
	res, err := h.Maps.UpdateSiteGeo(ctx, a, req.SiteId, maps.UpdateSiteGeoRequest{
		Lat:         req.Body.Lat,
		Lng:         req.Body.Lng,
		DefaultZoom: req.Body.DefaultZoom,
		RegionID:    req.Body.RegionId,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateSiteGeo200JSONResponse{
		Id:          res.ID,
		Name:        res.Name,
		Lat:         res.Lat,
		Lng:         res.Lng,
		DefaultZoom: res.DefaultZoom,
		RegionId:    res.RegionID,
		UpdatedAt:   &res.UpdatedAt,
	}, nil
}

func toMapPlacementResponse(p *maps.Placement) gen.MapPlacement {
	var props *map[string]interface{}
	if len(p.Props) > 0 {
		var pr map[string]interface{}
		if err := json.Unmarshal(p.Props, &pr); err == nil {
			props = &pr
		}
	}
	return gen.MapPlacement{
		Id:         p.ID,
		SiteId:     p.SiteID,
		EntityType: gen.MapPlacementEntityType(p.EntityType),
		EntityId:   p.EntityID,
		FloorId:    p.FloorID,
		Lat:        p.Lat,
		Lng:        p.Lng,
		X:          p.X,
		Y:          p.Y,
		BearingDeg: p.BearingDeg,
		FovDeg:     p.FovDeg,
		RangeM:     p.RangeM,
		Props:      props,
		Revision:   p.Revision,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func toMapSiteOverview(s maps.SiteOverview) gen.MapSiteOverview {
	return gen.MapSiteOverview{
		Id:              s.ID,
		Name:            s.Name,
		Lat:             s.Lat,
		Lng:             s.Lng,
		DefaultZoom:     s.DefaultZoom,
		RegionId:        s.RegionID,
		RegionName:      s.RegionName,
		CameraCount:     s.CameraCount,
		OnlineCameras:   s.OnlineCameras,
		OfflineCameras:  s.OfflineCameras,
		DegradedCameras: s.DegradedCameras,
		AlarmCount:      s.AlarmCount,
	}
}

func toMapSiteDetails(s *maps.SiteDetails) gen.MapSiteDetails {
	buildings := make([]gen.MapBuilding, 0, len(s.Buildings))
	for _, b := range s.Buildings {
		floors := make([]gen.MapFloor, 0, len(b.Floors))
		for _, f := range b.Floors {
			var georef *map[string]interface{}
			if len(f.Georef) > 0 {
				var g map[string]interface{}
				if err := json.Unmarshal(f.Georef, &g); err == nil {
					georef = &g
				}
			}
			floors = append(floors, gen.MapFloor{
				Id:              f.ID,
				BuildingId:      f.BuildingID,
				Name:            f.Name,
				Ordinal:         f.Ordinal,
				PlanKey:         f.PlanKey,
				PlanContentType: f.PlanContentType,
				PlanWidthPx:     f.PlanWidthPx,
				PlanHeightPx:    f.PlanHeightPx,
				Georef:          georef,
			})
		}
		var footprint *map[string]interface{}
		if len(b.Footprint) > 0 {
			var fp map[string]interface{}
			if err := json.Unmarshal(b.Footprint, &fp); err == nil {
				footprint = &fp
			}
		}
		buildings = append(buildings, gen.MapBuilding{
			Id:        b.ID,
			SiteId:    b.SiteID,
			Name:      b.Name,
			Footprint: footprint,
			Lat:       b.Lat,
			Lng:       b.Lng,
			Floors:    floors,
		})
	}

	zones := make([]gen.MapZone, 0, len(s.Zones))
	for _, z := range s.Zones {
		var geom map[string]interface{}
		if len(z.Geometry) > 0 {
			_ = json.Unmarshal(z.Geometry, &geom)
		}
		var style *map[string]interface{}
		if len(z.Style) > 0 {
			var st map[string]interface{}
			if err := json.Unmarshal(z.Style, &st); err == nil {
				style = &st
			}
		}
		var meta *map[string]interface{}
		if len(z.Metadata) > 0 {
			var m map[string]interface{}
			if err := json.Unmarshal(z.Metadata, &m); err == nil {
				meta = &m
			}
		}
		zones = append(zones, gen.MapZone{
			Id:       z.ID,
			SiteId:   z.SiteID,
			FloorId:  z.FloorID,
			Name:     z.Name,
			Kind:     gen.MapZoneKind(z.Kind),
			Geometry: geom,
			MinLat:   z.MinLat,
			MinLng:   z.MinLng,
			MaxLat:   z.MaxLat,
			MaxLng:   z.MaxLng,
			Style:    style,
			Metadata: meta,
		})
	}

	return gen.MapSiteDetails{
		Id:          s.ID,
		Name:        s.Name,
		Lat:         s.Lat,
		Lng:         s.Lng,
		DefaultZoom: s.DefaultZoom,
		RegionId:    s.RegionID,
		Buildings:   buildings,
		Zones:       zones,
	}
}

func toMapEntity(e maps.Entity) gen.MapEntity {
	pos := gen.MapPosition{
		K:       gen.MapPositionK(e.Position.Kind),
		Lat:     e.Position.Lat,
		Lng:     e.Position.Lng,
		FloorId: e.Position.FloorID,
		X:       e.Position.X,
		Y:       e.Position.Y,
	}

	var cam *gen.MapCameraProps
	if e.Camera != nil {
		cam = &gen.MapCameraProps{
			Bearing: e.Camera.Bearing,
			Fov:     e.Camera.Fov,
			Range:   e.Camera.Range,
			Type:    e.Camera.Type,
			Ptz:     e.Camera.PTZ,
			Lpr:     e.Camera.LPR,
		}
	}

	return gen.MapEntity{
		Id:     e.ID,
		T:      gen.MapEntityT(e.Type),
		Site:   e.SiteID,
		Srv:    e.ServerID,
		Name:   e.Name,
		Pos:    pos,
		Cam:    cam,
		St:     gen.MapEntitySt(e.Status),
		Alarms: e.Alarms,
	}
}
