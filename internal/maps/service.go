package maps

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Service provides maps use cases.
type Service struct {
	Store  *store.Store
	Config Config
	Log    *slog.Logger
}

// GetConfig returns the configured tile provider and default map settings.
func (s *Service) GetConfig(_ context.Context) (Config, error) {
	return s.Config, nil
}

// GetOverview lists all sites with geo coordinates and health summary visible to the actor.
func (s *Service) GetOverview(ctx context.Context, actor authz.Actor) ([]SiteOverview, error) {
	var out []SiteOverview
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		siteIDs, err := chk.SiteIDs(ctx, authz.MapsView)
		if err != nil {
			return err
		}
		if len(siteIDs) == 0 {
			return nil
		}

		var camFilter []uuid.UUID
		if !actor.IsPlatform() && (actor.TenantID == nil || !chk.Can(authz.CamerasView, access.Tenant(*actor.TenantID))) {
			cams, err := chk.CameraIDs(ctx, authz.CamerasView)
			if err != nil {
				return err
			}
			camFilter = cams
		}

		rows, err := q.ListMapSiteOverviews(ctx, db.ListMapSiteOverviewsParams{
			CameraIds: camFilter,
			TenantID:  actor.TenantID,
			SiteIds:   siteIDs,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, SiteOverview{
				ID:              r.ID,
				Name:            r.Name,
				Lat:             r.Lat,
				Lng:             r.Lng,
				DefaultZoom:     r.DefaultZoom,
				RegionID:        r.RegionID,
				RegionName:      r.RegionName,
				CameraCount:     int(r.CameraCount),
				OnlineCameras:   int(r.OnlineCameras),
				OfflineCameras:  int(r.OfflineCameras),
				DegradedCameras: int(r.DegradedCameras),
				AlarmCount:      int(r.AlarmCount),
			})
		}
		return nil
	})
	return out, err
}

// GetSiteDetails returns structural map data (bounds, buildings, floors, zones) for a site.
func (s *Service) GetSiteDetails(ctx context.Context, actor authz.Actor, siteID uuid.UUID) (*SiteDetails, error) {
	var details SiteDetails
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

		buildingsRows, err := q.ListMapBuildingsBySite(ctx, db.ListMapBuildingsBySiteParams{SiteID: siteID, TenantID: &site.TenantID})
		if err != nil {
			return store.Classify(err)
		}

		floorsRows, err := q.ListMapFloorsBySite(ctx, db.ListMapFloorsBySiteParams{SiteID: siteID, TenantID: &site.TenantID})
		if err != nil {
			return store.Classify(err)
		}

		zonesRows, err := q.ListMapZonesBySite(ctx, db.ListMapZonesBySiteParams{SiteID: siteID, TenantID: &site.TenantID})
		if err != nil {
			return store.Classify(err)
		}

		floorsByBuilding := make(map[uuid.UUID][]Floor)
		for _, f := range floorsRows {
			var w, h *int
			if f.PlanWidthPx != nil {
				val := int(*f.PlanWidthPx)
				w = &val
			}
			if f.PlanHeightPx != nil {
				val := int(*f.PlanHeightPx)
				h = &val
			}
			var planKey *string
			if f.PlanKey != "" {
				pk := f.PlanKey
				planKey = &pk
			}
			var planContentType *string
			if f.PlanContentType != "" {
				pct := f.PlanContentType
				planContentType = &pct
			}
			floorsByBuilding[f.BuildingID] = append(floorsByBuilding[f.BuildingID], Floor{
				ID:              f.ID,
				BuildingID:      f.BuildingID,
				Name:            f.Name,
				Ordinal:         int(f.Ordinal),
				PlanKey:         planKey,
				PlanContentType: planContentType,
				PlanWidthPx:     w,
				PlanHeightPx:    h,
				Georef:          f.Georef,
			})
		}

		buildings := make([]Building, 0, len(buildingsRows))
		for _, b := range buildingsRows {
			bFloors := floorsByBuilding[b.ID]
			if bFloors == nil {
				bFloors = []Floor{}
			}
			buildings = append(buildings, Building{
				ID:        b.ID,
				SiteID:    b.SiteID,
				Name:      b.Name,
				Footprint: b.Footprint,
				Lat:       b.Lat,
				Lng:       b.Lng,
				Floors:    bFloors,
			})
		}

		zones := make([]Zone, 0, len(zonesRows))
		for _, z := range zonesRows {
			zones = append(zones, Zone{
				ID:       z.ID,
				SiteID:   z.SiteID,
				FloorID:  z.FloorID,
				Name:     z.Name,
				Kind:     z.Kind,
				Geometry: z.Geometry,
				MinLat:   z.MinLat,
				MinLng:   z.MinLng,
				MaxLat:   z.MaxLat,
				MaxLng:   z.MaxLng,
				Style:    z.Style,
				Metadata: z.Metadata,
			})
		}

		details = SiteDetails{
			ID:          site.ID,
			Name:        site.Name,
			Lat:         site.Lat,
			Lng:         site.Lng,
			DefaultZoom: site.DefaultZoom,
			RegionID:    site.RegionID,
			Buildings:   buildings,
			Zones:       zones,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &details, nil
}

type placementProps struct {
	CameraType string `json:"camera_type"`
	PTZ        *bool  `json:"ptz"`
	LPR        *bool  `json:"lpr"`
}

// GetSiteEntities returns compact placed entities (cameras, servers, devices) for a site with RBAC filtering.
func (s *Service) GetSiteEntities(ctx context.Context, actor authz.Actor, siteID uuid.UUID, filter EntitiesFilter) (*EntitiesResult, error) {
	var res EntitiesResult
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

		rev, err := q.GetMapSiteRevision(ctx, db.GetMapSiteRevisionParams{SiteID: siteID, TenantID: &site.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		res.Revision = rev

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

		canViewAllServersOnSite := actor.IsPlatform() ||
			chk.Can(authz.ServersView, access.Tenant(site.TenantID)) ||
			chk.Can(authz.ServersView, access.Site(site.TenantID, siteID))

		var allowedServerIDs map[uuid.UUID]bool
		if !canViewAllServersOnSite {
			srvs, err := chk.ServerIDs(ctx, authz.ServersView)
			if err != nil {
				return err
			}
			allowedServerIDs = make(map[uuid.UUID]bool, len(srvs))
			for _, id := range srvs {
				allowedServerIDs[id] = true
			}
		}

		rows, err := q.ListMapPlacementsDetailed(ctx, db.ListMapPlacementsDetailedParams{
			SiteID:   siteID,
			TenantID: &site.TenantID,
			FloorID:  filter.FloorID,
			IsGeo:    filter.IsGeo,
			MinLat:   filter.MinLat,
			MaxLat:   filter.MaxLat,
			MinLng:   filter.MinLng,
			MaxLng:   filter.MaxLng,
		})
		if err != nil {
			return store.Classify(err)
		}

		res.Entities = make([]Entity, 0, len(rows))
		for _, r := range rows {
			var name string
			var status string
			var srvID *uuid.UUID

			switch r.EntityType {
			case "camera":
				if !canViewAllCamerasOnSite && !allowedCameraIDs[r.EntityID] {
					continue // RBAC: hide camera marker
				}
				if r.CameraName != nil {
					name = *r.CameraName
				}
				if r.CameraStatus != nil {
					status = *r.CameraStatus
				} else {
					status = "unknown"
				}
				srvID = r.CameraServerID
			case "server":
				if !canViewAllServersOnSite && !allowedServerIDs[r.EntityID] {
					continue // RBAC: hide server marker
				}
				if r.ServerName != nil {
					name = *r.ServerName
				}
				if r.ServerStatus != nil {
					status = *r.ServerStatus
				} else {
					status = "unknown"
				}
			case "device":
				if r.DeviceName != nil {
					name = *r.DeviceName
				}
				if r.DeviceStatus != nil {
					status = *r.DeviceStatus
				} else {
					status = "unknown"
				}
			default:
				continue
			}

			var pos Position
			if r.FloorID == nil {
				pos = Position{
					Kind: "geo",
					Lat:  r.Lat,
					Lng:  r.Lng,
				}
			} else {
				pos = Position{
					Kind:    "floor",
					FloorID: r.FloorID,
					X:       r.X,
					Y:       r.Y,
				}
			}

			var cam *CameraProps
			if r.EntityType == "camera" {
				var pProps placementProps
				if len(r.Props) > 0 {
					_ = json.Unmarshal(r.Props, &pProps)
				}
				camType := "fixed"
				if pProps.CameraType != "" {
					camType = pProps.CameraType
				}
				hasPTZ := (pProps.PTZ != nil && *pProps.PTZ) || camType == "ptz"
				hasLPR := (r.CameraLpr != nil && *r.CameraLpr) || (pProps.LPR != nil && *pProps.LPR)

				var bearing, fov, rangeM float32
				if r.BearingDeg != nil {
					bearing = *r.BearingDeg
				}
				if r.FovDeg != nil {
					fov = *r.FovDeg
				} else {
					fov = 70
				}
				if r.RangeM != nil {
					rangeM = *r.RangeM
				} else {
					rangeM = 30
				}

				cam = &CameraProps{
					Bearing: bearing,
					Fov:     fov,
					Range:   rangeM,
					Type:    camType,
					PTZ:     hasPTZ,
					LPR:     hasLPR,
				}
			}

			res.Entities = append(res.Entities, Entity{
				ID:       r.EntityID,
				Type:     r.EntityType,
				SiteID:   r.SiteID,
				ServerID: srvID,
				Name:     name,
				Position: pos,
				Camera:   cam,
				Status:   status,
				Alarms:   int(r.AlarmCount),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}
