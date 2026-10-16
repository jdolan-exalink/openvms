package rules

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// StoreZoneResolver resolves which map_zones apply to an event on a camera using
// camera placements (geo and floor polygons) and zone metadata in PostgreSQL.
type StoreZoneResolver struct {
	Store *store.Store
}

func NewStoreZoneResolver(st *store.Store) *StoreZoneResolver {
	if st == nil {
		return nil
	}
	return &StoreZoneResolver{Store: st}
}

type cameraPlacement struct {
	floorID *uuid.UUID
	lat     *float64
	lng     *float64
	x       *float32
	y       *float32
}

type siteMapZone struct {
	id       uuid.UUID
	floorID  *uuid.UUID
	name     string
	geometry []byte
	metadata []byte
}

type zoneMetadata struct {
	CameraIDs    []uuid.UUID `json:"camera_ids"`
	FrigateZones []string    `json:"frigate_zones"`
}

func (r *StoreZoneResolver) ZonesForEvent(ctx context.Context, cameraID uuid.UUID, frigateZones []string) ([]uuid.UUID, error) {
	if r == nil || r.Store == nil || cameraID == uuid.Nil {
		return nil, nil
	}
	var matched []uuid.UUID
	err := r.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		var tenantID, siteID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT tenant_id, site_id FROM cameras WHERE id = $1 AND deleted_at IS NULL`, cameraID).Scan(&tenantID, &siteID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		pRows, err := tx.Query(ctx, `
SELECT floor_id, lat, lng, x, y
FROM map_placements
WHERE tenant_id = $1 AND site_id = $2 AND entity_type = 'camera' AND entity_id = $3`, tenantID, siteID, cameraID)
		if err != nil {
			return err
		}
		var placements []cameraPlacement
		for pRows.Next() {
			var p cameraPlacement
			if err := pRows.Scan(&p.floorID, &p.lat, &p.lng, &p.x, &p.y); err != nil {
				pRows.Close()
				return err
			}
			placements = append(placements, p)
		}
		pRows.Close()
		if err := pRows.Err(); err != nil {
			return err
		}

		zRows, err := tx.Query(ctx, `
SELECT id, floor_id, name, geometry, metadata
FROM map_zones
WHERE tenant_id = $1 AND site_id = $2 AND deleted_at IS NULL`, tenantID, siteID)
		if err != nil {
			return err
		}
		var zones []siteMapZone
		for zRows.Next() {
			var z siteMapZone
			if err := zRows.Scan(&z.id, &z.floorID, &z.name, &z.geometry, &z.metadata); err != nil {
				zRows.Close()
				return err
			}
			zones = append(zones, z)
		}
		zRows.Close()
		if err := zRows.Err(); err != nil {
			return err
		}

		for _, z := range zones {
			if zoneMatchesCameraOrEvent(z, cameraID, placements, frigateZones) {
				matched = append(matched, z.id)
			}
		}
		return nil
	})
	return matched, err
}

func zoneMatchesCameraOrEvent(z siteMapZone, cameraID uuid.UUID, placements []cameraPlacement, frigateZones []string) bool {
	if len(z.metadata) > 0 {
		var meta zoneMetadata
		if json.Unmarshal(z.metadata, &meta) == nil {
			if containsID(meta.CameraIDs, cameraID) {
				return true
			}
			if len(meta.FrigateZones) > 0 && len(frigateZones) > 0 && matchesAny(meta.FrigateZones, frigateZones) {
				return true
			}
		}
	}
	if len(placements) == 0 || len(z.geometry) == 0 {
		return false
	}
	pts, _, err := maps.ValidatePolygon(z.geometry, z.floorID != nil)
	if err != nil {
		return false
	}
	for _, p := range placements {
		if z.floorID == nil && p.floorID == nil && p.lat != nil && p.lng != nil {
			if maps.PointInPolygon(maps.Point{X: *p.lng, Y: *p.lat}, pts) {
				return true
			}
		} else if z.floorID != nil && p.floorID != nil && *z.floorID == *p.floorID && p.x != nil && p.y != nil {
			if maps.PointInPolygon(maps.Point{X: float64(*p.x), Y: float64(*p.y)}, pts) {
				return true
			}
		}
	}
	return false
}
