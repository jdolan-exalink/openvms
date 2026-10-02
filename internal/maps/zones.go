package maps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ListZones returns all active map zones for a site.
func (s *Service) ListZones(ctx context.Context, actor authz.Actor, siteID uuid.UUID) ([]Zone, error) {
	var out []Zone
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsView, access.Site(site.TenantID, siteID)); err != nil {
			return err
		}

		rows, err := q.ListMapZonesBySite(ctx, db.ListMapZonesBySiteParams{
			SiteID:   siteID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, toZone(r))
		}
		return nil
	})
	return out, err
}

// GetZone returns a single map zone by ID.
func (s *Service) GetZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) (*Zone, error) {
	var out Zone
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		r, err := q.GetMapZone(ctx, db.GetMapZoneParams{
			ID:       zoneID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsView, access.Site(r.TenantID, r.SiteID)); err != nil {
			return err
		}
		out = toZone(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateZone validates geometry and creates a new map zone.
func (s *Service) CreateZone(ctx context.Context, actor authz.Actor, siteID uuid.UUID, req CreateZoneRequest) (*Zone, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, &ValidationError{Msg: "zone name is required"}
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "security" && kind != "perimeter" && kind != "warning" && kind != "custom" {
		return nil, &ValidationError{Msg: "invalid zone kind: must be security, perimeter, warning, or custom"}
	}

	isFloor := req.FloorID != nil
	_, bbox, err := ValidatePolygon(req.Geometry, isFloor)
	if err != nil {
		return nil, err
	}

	var minLat, minLng, maxLat, maxLng *float64
	if bbox != nil {
		minLat = bbox.MinLat
		minLng = bbox.MinLng
		maxLat = bbox.MaxLat
		maxLng = bbox.MaxLng
	}

	style := req.Style
	if len(style) == 0 {
		style = json.RawMessage("{}")
	}
	metadata := req.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	var out Zone
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsCreateZone, access.Site(site.TenantID, siteID)); err != nil {
			return err
		}

		if err := validateFloorOnSite(ctx, q, site.TenantID, siteID, req.FloorID); err != nil {
			return err
		}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		row, err := q.CreateMapZone(ctx, db.CreateMapZoneParams{
			TenantID: site.TenantID,
			SiteID:   siteID,
			FloorID:  req.FloorID,
			Name:     name,
			Kind:     kind,
			Geometry: req.Geometry,
			MinLat:   minLat,
			MinLng:   minLng,
			MaxLat:   maxLat,
			MaxLng:   maxLng,
			Style:    style,
			Metadata: metadata,
			UserID:   uid,
		})
		if err != nil {
			return store.Classify(err)
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"site_id":  siteID,
			"floor_id": req.FloorID,
			"name":     name,
			"kind":     kind,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &site.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.zone.create",
			TargetType: "map_zone",
			TargetID:   &row.ID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})

		out = toZone(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateZone updates an existing map zone.
func (s *Service) UpdateZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID, req UpdateZoneRequest) (*Zone, error) {
	var out Zone
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		zone, err := q.GetMapZone(ctx, db.GetMapZoneParams{
			ID:       zoneID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsCreateZone, access.Site(zone.TenantID, zone.SiteID)); err != nil {
			return err
		}

		var name *string
		if req.Name != nil {
			trimmed := strings.TrimSpace(*req.Name)
			if trimmed == "" {
				return &ValidationError{Msg: "zone name cannot be empty"}
			}
			name = &trimmed
		}

		var kind *string
		if req.Kind != nil {
			k := strings.TrimSpace(*req.Kind)
			if k != "security" && k != "perimeter" && k != "warning" && k != "custom" {
				return &ValidationError{Msg: "invalid zone kind: must be security, perimeter, warning, or custom"}
			}
			kind = &k
		}

		floorID := zone.FloorID
		if req.FloorID != nil {
			floorID = req.FloorID
		}
		if err := validateFloorOnSite(ctx, q, zone.TenantID, zone.SiteID, floorID); err != nil {
			return err
		}
		isFloor := floorID != nil

		var geom json.RawMessage
		var minLat, minLng, maxLat, maxLng *float64
		if len(req.Geometry) > 0 {
			_, bbox, err := ValidatePolygon(req.Geometry, isFloor)
			if err != nil {
				return err
			}
			geom = req.Geometry
			if bbox != nil {
				minLat = bbox.MinLat
				minLng = bbox.MinLng
				maxLat = bbox.MaxLat
				maxLng = bbox.MaxLng
			}
		}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		row, err := q.UpdateMapZone(ctx, db.UpdateMapZoneParams{
			ID:       zoneID,
			TenantID: actor.TenantID,
			Name:     name,
			Kind:     kind,
			Geometry: geom,
			FloorID:  req.FloorID,
			MinLat:   minLat,
			MinLng:   minLng,
			MaxLat:   maxLat,
			MaxLng:   maxLng,
			Style:    req.Style,
			Metadata: req.Metadata,
			UserID:   uid,
		})
		if err != nil {
			return store.Classify(err)
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"site_id":  zone.SiteID,
			"floor_id": floorID,
			"name":     row.Name,
			"kind":     row.Kind,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &zone.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.zone.update",
			TargetType: "map_zone",
			TargetID:   &zoneID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})

		out = toZone(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteZone soft-deletes a map zone.
func (s *Service) DeleteZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		zone, err := q.GetMapZone(ctx, db.GetMapZoneParams{
			ID:       zoneID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsCreateZone, access.Site(zone.TenantID, zone.SiteID)); err != nil {
			return err
		}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		if err := q.DeleteMapZone(ctx, db.DeleteMapZoneParams{
			ID:       zoneID,
			TenantID: actor.TenantID,
			UserID:   uid,
		}); err != nil {
			return store.Classify(err)
		}

		auditPayload, _ := json.Marshal(map[string]interface{}{
			"site_id": zone.SiteID,
			"name":    zone.Name,
		})
		_ = q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &zone.TenantID,
			ActorID:    uid,
			ActorName:  actor.Username,
			Action:     "maps.zone.delete",
			TargetType: "map_zone",
			TargetID:   &zoneID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    auditPayload,
		})
		return nil
	})
}

func toZone(r db.MapZone) Zone {
	return Zone{
		ID:       r.ID,
		SiteID:   r.SiteID,
		FloorID:  r.FloorID,
		Name:     r.Name,
		Kind:     r.Kind,
		Geometry: r.Geometry,
		MinLat:   r.MinLat,
		MinLng:   r.MinLng,
		MaxLat:   r.MaxLat,
		MaxLng:   r.MaxLng,
		Style:    r.Style,
		Metadata: r.Metadata,
	}
}
