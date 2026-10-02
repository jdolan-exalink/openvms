package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func mapName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return "", &ValidationError{Msg: "map name must contain 1 to 120 characters"}
	}
	return name, nil
}
func matchRevision(header string, revision int64) error {
	// Unconditional '*' would make replacement/deletion unsafe for concurrently edited plans.
	if strings.Trim(strings.TrimSpace(header), `"`) != fmt.Sprint(revision) {
		return ErrOptimisticLockConflict
	}
	return nil
}
func auditMap(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID, id uuid.UUID, kind, action string, revision int64) error {
	var uid *uuid.UUID
	if actor.UserID != uuid.Nil {
		uid = &actor.UserID
	}
	payload, _ := json.Marshal(map[string]any{"revision": revision})
	return q.InsertAudit(ctx, db.InsertAuditParams{TenantID: &tenantID, ActorID: uid, ActorName: actor.Username, Action: action, TargetType: kind, TargetID: &id, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: payload})
}
func mapSite(ctx context.Context, q *db.Queries, actor authz.Actor, siteID uuid.UUID, permission authz.Permission) (db.GetSiteGeoRow, error) {
	site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
	if err != nil {
		return site, store.Classify(err)
	}
	chk, err := access.Load(ctx, q, actor)
	if err != nil {
		return site, err
	}
	return site, chk.Require(permission, access.Site(site.TenantID, siteID))
}

// SaveBuilding creates a named plant/city grouping or renames an existing one.
func (s *Service) SaveBuilding(ctx context.Context, actor authz.Actor, siteID uuid.UUID, id *uuid.UUID, name, ifMatch string) (*Building, error) {
	name, err := mapName(name)
	if err != nil {
		return nil, err
	}
	var result Building
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsEdit)
		if err != nil {
			return err
		}
		var row db.MapBuilding
		if id == nil {
			row, err = q.CreateMapBuilding(ctx, db.CreateMapBuildingParams{TenantID: site.TenantID, SiteID: siteID, Name: name})
		} else {
			old, e := q.GetMapBuildingOnSite(ctx, db.GetMapBuildingOnSiteParams{ID: *id, TenantID: site.TenantID, SiteID: siteID})
			if e != nil {
				return store.Classify(e)
			}
			if e = matchRevision(ifMatch, old.Revision); e != nil {
				return e
			}
			row, err = q.UpdateMapBuilding(ctx, db.UpdateMapBuildingParams{ID: *id, TenantID: site.TenantID, Name: &name})
		}
		if err != nil {
			return store.Classify(err)
		}
		if err = auditMap(ctx, q, actor, site.TenantID, row.ID, "map_building", "maps.building.upsert", row.Revision); err != nil {
			return err
		}
		result = buildingModel(row)
		return nil
	})
	return &result, err
}
func (s *Service) DeleteBuilding(ctx context.Context, actor authz.Actor, siteID, id uuid.UUID, ifMatch string) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsEdit)
		if err != nil {
			return err
		}
		row, err := q.GetMapBuildingOnSite(ctx, db.GetMapBuildingOnSiteParams{ID: id, TenantID: site.TenantID, SiteID: siteID})
		if err != nil {
			return store.Classify(err)
		}
		if err = matchRevision(ifMatch, row.Revision); err != nil {
			return err
		}
		floors, err := q.ListMapFloorsByBuilding(ctx, db.ListMapFloorsByBuildingParams{BuildingID: id, TenantID: site.TenantID})
		if err != nil {
			return err
		}
		if len(floors) > 0 {
			return &ValidationError{Msg: "remove the building's floor maps before deleting it"}
		}
		deleted, err := q.DeleteMapBuilding(ctx, db.DeleteMapBuildingParams{ID: id, TenantID: site.TenantID})
		if err != nil {
			return err
		}
		return auditMap(ctx, q, actor, site.TenantID, id, "map_building", "maps.building.delete", deleted.Revision)
	})
}

// SaveFloor keeps each camera's independent geo and other-floor placements intact.
func (s *Service) SaveFloor(ctx context.Context, actor authz.Actor, siteID, buildingID uuid.UUID, id *uuid.UUID, name string, ordinal int, ifMatch string) (*Floor, error) {
	name, err := mapName(name)
	if err != nil {
		return nil, err
	}
	if ordinal < -10000 || ordinal > 10000 {
		return nil, &ValidationError{Msg: "ordinal must be between -10000 and 10000"}
	}
	var result Floor
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsEdit)
		if err != nil {
			return err
		}
		var row db.MapFloor
		if id == nil {
			if _, err = q.GetMapBuildingOnSite(ctx, db.GetMapBuildingOnSiteParams{ID: buildingID, TenantID: site.TenantID, SiteID: siteID}); err != nil {
				return store.Classify(err)
			}
			row, err = q.CreateMapFloor(ctx, db.CreateMapFloorParams{TenantID: site.TenantID, BuildingID: buildingID, Name: name, Ordinal: int32(ordinal)})
		} else {
			old, e := q.LockMapFloorOnSite(ctx, db.LockMapFloorOnSiteParams{ID: *id, TenantID: site.TenantID, SiteID: siteID})
			if e != nil {
				return store.Classify(e)
			}
			if e = matchRevision(ifMatch, old.Revision); e != nil {
				return e
			}
			row, err = q.UpdateMapFloor(ctx, db.UpdateMapFloorParams{ID: *id, TenantID: site.TenantID, Name: &name, Ordinal: ptrOrdinal(ordinal)})
		}
		if err != nil {
			return store.Classify(err)
		}
		if err = auditMap(ctx, q, actor, site.TenantID, row.ID, "map_floor", "maps.floor.upsert", row.Revision); err != nil {
			return err
		}
		result = floorModel(row)
		return nil
	})
	return &result, err
}
func (s *Service) DeleteFloor(ctx context.Context, actor authz.Actor, siteID, id uuid.UUID, ifMatch string) error {
	var key string
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsEdit)
		if err != nil {
			return err
		}
		row, err := q.LockMapFloorOnSite(ctx, db.LockMapFloorOnSiteParams{ID: id, TenantID: site.TenantID, SiteID: siteID})
		if err != nil {
			return store.Classify(err)
		}
		if err = matchRevision(ifMatch, row.Revision); err != nil {
			return err
		}
		occupied, err := q.MapFloorHasContent(ctx, db.MapFloorHasContentParams{FloorID: id, TenantID: site.TenantID})
		if err != nil {
			return err
		}
		if occupied {
			return &ValidationError{Msg: "remove this floor's placements and zones before deleting it"}
		}
		deleted, err := q.DeleteMapFloor(ctx, db.DeleteMapFloorParams{ID: id, TenantID: site.TenantID})
		if err != nil {
			return err
		}
		if trustedPlanKey(site.TenantID, id, row.PlanKey) {
			key = row.PlanKey
		}
		return auditMap(ctx, q, actor, site.TenantID, id, "map_floor", "maps.floor.delete", deleted.Revision)
	})
	if err == nil {
		s.removePlanBlob(ctx, key)
	}
	return err
}
func buildingModel(r db.MapBuilding) Building {
	return Building{ID: r.ID, SiteID: r.SiteID, Name: r.Name, Lat: r.Lat, Lng: r.Lng, Footprint: r.Footprint, Revision: r.Revision, Floors: []Floor{}}
}
func floorModel(r db.MapFloor) Floor {
	f := Floor{ID: r.ID, BuildingID: r.BuildingID, Name: r.Name, Ordinal: int(r.Ordinal), Georef: r.Georef, Revision: r.Revision}
	if r.PlanKey != "" {
		f.PlanKey = &r.PlanKey
		f.PlanContentType = &r.PlanContentType
	}
	if r.PlanWidthPx != nil {
		v := int(*r.PlanWidthPx)
		f.PlanWidthPx = &v
	}
	if r.PlanHeightPx != nil {
		v := int(*r.PlanHeightPx)
		f.PlanHeightPx = &v
	}
	return f
}

func ptrOrdinal(n int) *int32 { v := int32(n); return &v }
