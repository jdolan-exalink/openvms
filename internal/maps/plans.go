package maps

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// PlanBlobs is private storage; all access goes through site authorization, never signed URLs.
type PlanBlobs interface {
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, string, error)
	Delete(context.Context, string) error
}

func (s *Service) removePlanBlob(ctx context.Context, key string) {
	if key == "" || s.Blobs == nil {
		return
	}
	if err := s.Blobs.Delete(ctx, key); err != nil && s.Log != nil {
		s.Log.WarnContext(ctx, "map plan cleanup failed", "error", err)
	}
}
func (s *Service) UploadFloorPlan(ctx context.Context, actor authz.Actor, siteID, floorID uuid.UUID, ifMatch string, body io.Reader) (*Floor, error) {
	if s.Blobs == nil {
		return nil, &ValidationError{Msg: "map plan storage is unavailable"}
	}
	if body == nil {
		return nil, &ValidationError{Msg: "PNG body is required"}
	}
	var result Floor
	var uploaded, previous string
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsEdit)
		if err != nil {
			return err
		}
		floor, err := q.LockMapFloorOnSite(ctx, db.LockMapFloorOnSiteParams{ID: floorID, TenantID: site.TenantID, SiteID: siteID})
		if err != nil {
			return store.Classify(err)
		}
		if err = matchRevision(ifMatch, floor.Revision); err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(body, MaxPlanBytes+1))
		if err != nil {
			return err
		}
		data, w, h, err := canonicalPlanPNG(data)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("tenant/%s/maps/floors/%s/%s.png", site.TenantID, floorID, uuid.New())
		// Set before Put: a failed/uncertain write can be safely removed by this unique key.
		uploaded = key
		if err = s.Blobs.Put(ctx, key, data, "image/png"); err != nil {
			return err
		}
		contentType := "image/png"
		row, err := q.UpdateMapFloor(ctx, db.UpdateMapFloorParams{ID: floorID, TenantID: site.TenantID, PlanKey: &key, PlanContentType: &contentType, PlanWidthPx: &w, PlanHeightPx: &h})
		if err != nil {
			return store.Classify(err)
		}
		if err = auditMap(ctx, q, actor, site.TenantID, floorID, "map_floor", "maps.floor.plan.upload", row.Revision); err != nil {
			return err
		}
		if trustedPlanKey(site.TenantID, floorID, floor.PlanKey) {
			previous = floor.PlanKey
		}
		result = floorModel(row)
		return nil
	})
	if err != nil {
		s.removePlanBlob(ctx, uploaded)
		return nil, err
	}
	s.removePlanBlob(ctx, previous)
	return &result, nil
}
func (s *Service) GetFloorPlan(ctx context.Context, actor authz.Actor, siteID, floorID uuid.UUID) ([]byte, error) {
	var data []byte
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		site, err := mapSite(ctx, q, actor, siteID, authz.MapsView)
		if err != nil {
			return err
		}
		floor, err := q.GetMapFloorOnSite(ctx, db.GetMapFloorOnSiteParams{ID: floorID, TenantID: site.TenantID, SiteID: siteID})
		if err != nil {
			return store.Classify(err)
		}
		if floor.PlanKey == "" || floor.PlanContentType != "image/png" || !trustedPlanKey(site.TenantID, floorID, floor.PlanKey) || s.Blobs == nil {
			return store.ErrNotFound
		}
		data, _, err = s.Blobs.Get(ctx, floor.PlanKey)
		if err != nil {
			return err
		}
		// Legacy/corrupt object contents are never served unchecked as trusted images.
		data, _, _, err = canonicalPlanPNG(data)
		return err
	})
	return data, err
}

func trustedPlanKey(tenantID, floorID uuid.UUID, key string) bool {
	prefix := fmt.Sprintf("tenant/%s/maps/floors/%s/", tenantID, floorID)
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, ".png") {
		return false
	}
	_, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".png"))
	return err == nil
}
