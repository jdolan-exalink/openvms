package control

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// SiteServer implements openvmsv1.SiteServiceServer.
type SiteServer struct {
	openvmsv1.UnimplementedSiteServiceServer
	Inv      *inventory.Service
	Identity *identity.Service
}

func (s *SiteServer) ListSites(ctx context.Context, req *openvmsv1.ListSitesRequest) (*openvmsv1.ListSitesResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	sites, err := s.Inv.ListSites(ctx, actor, actor.TenantID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list sites: %v", err)
	}

	pbSites := make([]*openvmsv1.Site, 0, len(sites))
	for _, st := range sites {
		var lat, lng float64
		if st.Lat != nil {
			lat = *st.Lat
		}
		if st.Lng != nil {
			lng = *st.Lng
		}

		tenantIDStr := ""
		if st.TenantID != uuid.Nil {
			tenantIDStr = st.TenantID.String()
		}

		pbSites = append(pbSites, &openvmsv1.Site{
			Id:             st.ID.String(),
			OrganizationId: tenantIDStr,
			Name:           st.Name,
			Timezone:       st.Timezone,
			Latitude:       lat,
			Longitude:      lng,
			Status:         openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE,
			CreatedAt:      st.CreatedAt.Unix(),
			CameraCount:    int32(st.CameraCount),
			NodeCount:      int32(st.ServerCount),
		})
	}

	return &openvmsv1.ListSitesResponse{
		Sites: pbSites,
		Pagination: &openvmsv1.Pagination{
			Page:       1,
			PageSize:   int32(len(pbSites)),
			TotalItems: int64(len(pbSites)),
			TotalPages: 1,
		},
	}, nil
}

func (s *SiteServer) GetSite(ctx context.Context, req *openvmsv1.GetSiteRequest) (*openvmsv1.GetSiteResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	siteID, err := uuid.Parse(req.SiteId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid site_id")
	}

	st, err := s.Inv.GetSite(ctx, actor, siteID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "site not found: %v", err)
	}

	var lat, lng float64
	if st.Lat != nil {
		lat = *st.Lat
	}
	if st.Lng != nil {
		lng = *st.Lng
	}

	tenantIDStr := ""
	if st.TenantID != uuid.Nil {
		tenantIDStr = st.TenantID.String()
	}

	return &openvmsv1.GetSiteResponse{
		Site: &openvmsv1.Site{
			Id:             st.ID.String(),
			OrganizationId: tenantIDStr,
			Name:           st.Name,
			Timezone:       st.Timezone,
			Latitude:       lat,
			Longitude:      lng,
			Status:         openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE,
			CreatedAt:      st.CreatedAt.Unix(),
			CameraCount:    int32(st.CameraCount),
			NodeCount:      int32(st.ServerCount),
		},
	}, nil
}
