package maps

import (
	"context"
	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// The shared locks keep hierarchy deletion from racing a placement or zone write.
func validateFloorOnSite(ctx context.Context, q *db.Queries, tenantID, siteID uuid.UUID, floorID *uuid.UUID) error {
	if floorID == nil {
		return nil
	}
	_, err := q.GetMapFloorOnSite(ctx, db.GetMapFloorOnSiteParams{ID: *floorID, TenantID: tenantID, SiteID: siteID})
	return store.Classify(err)
}
