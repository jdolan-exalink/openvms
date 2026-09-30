package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// GetUnplacedCameras returns cameras on the site that have no geographic placement.
func (s *Service) GetUnplacedCameras(ctx context.Context, actor authz.Actor, siteID uuid.UUID) ([]UnplacedCamera, error) {
	var out []UnplacedCamera
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsEdit, access.Site(site.TenantID, siteID)); err != nil {
			return err
		}

		rows, err := q.ListUnplacedCamerasBySite(ctx, db.ListUnplacedCamerasBySiteParams{
			SiteID:   siteID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return err
		}

		canViewAllCamerasOnSite := actor.IsPlatform() ||
			chk.Can(authz.CamerasView, access.Tenant(site.TenantID)) ||
			chk.Can(authz.CamerasView, access.Site(site.TenantID, siteID))

		var allowedCameraIDs map[uuid.UUID]bool
		if !canViewAllCamerasOnSite {
			cams, err := chk.CameraIDs(ctx, authz.CamerasView)
			if err != nil {
				return err
			}
			allowedCameraIDs = make(map[uuid.UUID]bool, len(cams))
			for _, id := range cams {
				allowedCameraIDs[id] = true
			}
		}

		for _, r := range rows {
			if !canViewAllCamerasOnSite && !allowedCameraIDs[r.ID] {
				continue
			}
			var remoteName *string
			if r.RemoteName != "" {
				remoteName = &r.RemoteName
			}
			out = append(out, UnplacedCamera{
				ID:         r.ID,
				Name:       r.DisplayName,
				RemoteName: remoteName,
				SiteID:     r.SiteID,
				Status:     r.Status,
			})
		}
		return nil
	})
	return out, err
}

// UpsertPlacement creates or updates a geographic or floor placement for an entity.
func (s *Service) UpsertPlacement(ctx context.Context, actor authz.Actor, entityType string, entityID uuid.UUID, ifMatch *string, req UpsertPlacementRequest) (*Placement, error) {
	if entityType != "camera" && entityType != "server" && entityType != "device" {
		return nil, &ValidationError{Msg: "invalid entity_type: must be camera, server, or device"}
	}

	if req.FloorID == nil {
		// Geographic placement validation
		if req.Lat == nil || req.Lng == nil {
			return nil, &ValidationError{Msg: "lat and lng are required for geo placement"}
		}
		if *req.Lat < -90 || *req.Lat > 90 {
			return nil, &ValidationError{Msg: "lat must be between -90 and 90"}
		}
		if *req.Lng < -180 || *req.Lng > 180 {
			return nil, &ValidationError{Msg: "lng must be between -180 and 180"}
		}
		if req.X != nil || req.Y != nil {
			return nil, &ValidationError{Msg: "x and y must be null for geo placement"}
		}
	} else {
		// Floor placement validation
		if req.X == nil || req.Y == nil {
			return nil, &ValidationError{Msg: "x and y are required for floor placement"}
		}
		if *req.X < 0 || *req.X > 1 {
			return nil, &ValidationError{Msg: "x must be between 0 and 1"}
		}
		if *req.Y < 0 || *req.Y > 1 {
			return nil, &ValidationError{Msg: "y must be between 0 and 1"}
		}
		if req.Lat != nil || req.Lng != nil {
			return nil, &ValidationError{Msg: "lat and lng must be null for floor placement"}
		}
	}

	if req.BearingDeg != nil && (*req.BearingDeg < 0 || *req.BearingDeg > 360) {
		return nil, &ValidationError{Msg: "bearing_deg must be between 0 and 360"}
	}
	if req.FovDeg != nil && (*req.FovDeg < 1 || *req.FovDeg > 360) {
		return nil, &ValidationError{Msg: "fov_deg must be between 1 and 360"}
	}
	if req.RangeM != nil && *req.RangeM < 0 {
		return nil, &ValidationError{Msg: "range_m must be non-negative"}
	}

	var res Placement
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: req.SiteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsEditDevice, access.Site(site.TenantID, req.SiteID)); err != nil {
			return err
		}

		// Verify entity exists and belongs to the site
		switch entityType {
		case "camera":
			cam, err := q.GetCamera(ctx, entityID)
			if err != nil {
				return store.Classify(err)
			}
			if cam.SiteID != req.SiteID {
				return &ValidationError{Msg: "camera does not belong to specified site"}
			}
		case "server":
			srv, err := q.GetServer(ctx, entityID)
			if err != nil {
				return store.Classify(err)
			}
			if srv.SiteID != req.SiteID {
				return &ValidationError{Msg: "server does not belong to specified site"}
			}
		case "device":
			dev, err := q.GetMapDevice(ctx, db.GetMapDeviceParams{ID: entityID, TenantID: site.TenantID})
			if err != nil {
				return store.Classify(err)
			}
			if dev.SiteID != req.SiteID {
				return &ValidationError{Msg: "device does not belong to specified site"}
			}
		}

		// Optimistic concurrency check (If-Match)
		var existing db.MapPlacement
		var existErr error
		if req.FloorID == nil {
			existing, existErr = q.GetMapPlacementByEntityGeo(ctx, db.GetMapPlacementByEntityGeoParams{
				EntityType: entityType,
				EntityID:   entityID,
				TenantID:   actor.TenantID,
			})
		} else {
			existing, existErr = q.GetMapPlacementByEntityFloor(ctx, db.GetMapPlacementByEntityFloorParams{
				EntityType: entityType,
				EntityID:   entityID,
				FloorID:    req.FloorID,
				TenantID:   actor.TenantID,
			})
		}

		if existErr == nil {
			if ifMatch != nil && *ifMatch != "" && *ifMatch != "*" {
				match := strings.Trim(strings.TrimSpace(*ifMatch), `"`)
				if fmt.Sprintf("%d", existing.Revision) != match {
					return ErrOptimisticLockConflict
				}
			}
		} else if errors.Is(store.Classify(existErr), store.ErrNotFound) {
			if ifMatch != nil && *ifMatch != "" {
				// Header was sent, but resource does not exist yet
				return ErrOptimisticLockConflict
			}
		} else {
			return store.Classify(existErr)
		}

		var row db.MapPlacement
		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		propsJSON := req.Props
		if len(propsJSON) == 0 {
			propsJSON = json.RawMessage("{}")
		}

		if req.FloorID == nil {
			row, err = q.UpsertGeoPlacement(ctx, db.UpsertGeoPlacementParams{
				TenantID:   site.TenantID,
				SiteID:     req.SiteID,
				EntityType: entityType,
				EntityID:   entityID,
				Lat:        req.Lat,
				Lng:        req.Lng,
				BearingDeg: req.BearingDeg,
				FovDeg:     req.FovDeg,
				RangeM:     req.RangeM,
				Props:      propsJSON,
				UserID:     uid,
			})
		} else {
			row, err = q.UpsertFloorPlacement(ctx, db.UpsertFloorPlacementParams{
				TenantID:   site.TenantID,
				SiteID:     req.SiteID,
				EntityType: entityType,
				EntityID:   entityID,
				FloorID:    req.FloorID,
				X:          req.X,
				Y:          req.Y,
				BearingDeg: req.BearingDeg,
				FovDeg:     req.FovDeg,
				RangeM:     req.RangeM,
				Props:      propsJSON,
				UserID:     uid,
			})
		}
		if err != nil {
			return store.Classify(err)
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": entityType,
			"entity_id":   entityID,
			"site_id":     req.SiteID,
			"floor_id":    req.FloorID,
			"lat":         req.Lat,
			"lng":         req.Lng,
			"x":           req.X,
			"y":           req.Y,
			"bearing_deg": req.BearingDeg,
			"fov_deg":     req.FovDeg,
			"range_m":     req.RangeM,
			"revision":    row.Revision,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &site.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.placement.upsert",
			TargetType: "map_placement",
			TargetID:   &row.ID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})

		res = toPlacement(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// DeletePlacement removes a map placement by ID.
func (s *Service) DeletePlacement(ctx context.Context, actor authz.Actor, placementID uuid.UUID, ifMatch *string) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		placement, err := q.GetMapPlacement(ctx, db.GetMapPlacementParams{
			ID:       placementID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsEditDevice, access.Site(placement.TenantID, placement.SiteID)); err != nil {
			return err
		}

		if ifMatch != nil && *ifMatch != "" && *ifMatch != "*" {
			match := strings.Trim(strings.TrimSpace(*ifMatch), `"`)
			if fmt.Sprintf("%d", placement.Revision) != match {
				return ErrOptimisticLockConflict
			}
		}

		if err := q.DeleteMapPlacement(ctx, db.DeleteMapPlacementParams{
			ID:       placementID,
			TenantID: actor.TenantID,
		}); err != nil {
			return store.Classify(err)
		}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": placement.EntityType,
			"entity_id":   placement.EntityID,
			"site_id":     placement.SiteID,
			"floor_id":    placement.FloorID,
			"revision":    placement.Revision,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &placement.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.placement.delete",
			TargetType: "map_placement",
			TargetID:   &placement.ID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})

		return nil
	})
}

// UpdateSiteGeo updates geographic coordinates, default zoom, and region for a site.
func (s *Service) UpdateSiteGeo(ctx context.Context, actor authz.Actor, siteID uuid.UUID, req UpdateSiteGeoRequest) (*SiteGeo, error) {
	if req.Lat != nil && (*req.Lat < -90 || *req.Lat > 90) {
		return nil, &ValidationError{Msg: "lat must be between -90 and 90"}
	}
	if req.Lng != nil && (*req.Lng < -180 || *req.Lng > 180) {
		return nil, &ValidationError{Msg: "lng must be between -180 and 180"}
	}
	if req.DefaultZoom != nil && (*req.DefaultZoom < 0 || *req.DefaultZoom > 24) {
		return nil, &ValidationError{Msg: "default_zoom must be between 0 and 24"}
	}

	var res SiteGeo
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsEdit, access.Site(site.TenantID, siteID)); err != nil {
			return err
		}

		if req.RegionID != nil {
			if _, err := q.GetMapRegion(ctx, db.GetMapRegionParams{
				ID:       *req.RegionID,
				TenantID: site.TenantID,
			}); err != nil {
				return &ValidationError{Msg: "region does not exist"}
			}
		}

		row, err := q.UpdateSiteGeo(ctx, db.UpdateSiteGeoParams{
			ID:          siteID,
			TenantID:    actor.TenantID,
			Lat:         req.Lat,
			Lng:         req.Lng,
			DefaultZoom: req.DefaultZoom,
			RegionID:    req.RegionID,
		})
		if err != nil {
			return store.Classify(err)
		}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"lat":          req.Lat,
			"lng":          req.Lng,
			"default_zoom": req.DefaultZoom,
			"region_id":    req.RegionID,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &site.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.site.geo_update",
			TargetType: "site",
			TargetID:   &siteID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})

		res = SiteGeo{
			ID:          row.ID,
			Name:        row.Name,
			Lat:         row.Lat,
			Lng:         row.Lng,
			DefaultZoom: row.DefaultZoom,
			RegionID:    row.RegionID,
			UpdatedAt:   row.UpdatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func toPlacement(r db.MapPlacement) Placement {
	return Placement{
		ID:         r.ID,
		SiteID:     r.SiteID,
		EntityType: r.EntityType,
		EntityID:   r.EntityID,
		FloorID:    r.FloorID,
		Lat:        r.Lat,
		Lng:        r.Lng,
		X:          r.X,
		Y:          r.Y,
		BearingDeg: r.BearingDeg,
		FovDeg:     r.FovDeg,
		RangeM:     r.RangeM,
		Props:      r.Props,
		Revision:   r.Revision,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
