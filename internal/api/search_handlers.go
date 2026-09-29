package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func (h *Handlers) GlobalSearch(ctx context.Context, r gen.GlobalSearchRequestObject) (gen.GlobalSearchResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.Search.Search(ctx, a, r.Params.Q)
	if err != nil {
		return nil, err
	}

	out := gen.SearchResult{
		Cameras: make([]gen.SearchCameraHit, len(res.Cameras)),
		Sites:   make([]gen.SearchSiteHit, len(res.Sites)),
		Servers: make([]gen.SearchServerHit, len(res.Servers)),
		Events:  make([]gen.SearchEventHit, len(res.Events)),
		Plates:  make([]gen.SearchPlateHit, len(res.Plates)),
	}

	for i, c := range res.Cameras {
		var loc *string
		if c.Location != "" {
			loc = &c.Location
		}
		out.Cameras[i] = gen.SearchCameraHit{
			Id:          c.ID,
			DisplayName: c.DisplayName,
			SiteId:      c.SiteID,
			SiteName:    c.SiteName,
			Location:    loc,
			Status:      gen.HealthStatus(c.Status),
		}
	}

	for i, s := range res.Sites {
		out.Sites[i] = gen.SearchSiteHit{
			Id:          s.ID,
			Name:        s.Name,
			CameraCount: s.CameraCount,
		}
	}

	for i, s := range res.Servers {
		out.Servers[i] = gen.SearchServerHit{
			Id:       s.ID,
			Name:     s.Name,
			SiteId:   s.SiteID,
			SiteName: s.SiteName,
			Status:   gen.HealthStatus(s.Status),
		}
	}

	for i, e := range res.Events {
		labels := e.Labels
		if labels == nil {
			labels = []string{}
		}
		out.Events[i] = gen.SearchEventHit{
			Id:           e.ID,
			CameraId:     e.CameraID,
			CameraName:   e.CameraName,
			SiteName:     e.SiteName,
			Severity:     gen.Severity(e.Severity),
			Labels:       labels,
			StartTime:    e.StartTime,
			HasThumbnail: e.HasThumbnail,
		}
	}

	for i, p := range res.Plates {
		out.Plates[i] = gen.SearchPlateHit{
			Plate:      p.Plate,
			CameraId:   p.CameraID,
			CameraName: p.CameraName,
			SiteName:   p.SiteName,
			SeenAt:     p.SeenAt,
			EventId:    p.EventID,
		}
	}

	return gen.GlobalSearch200JSONResponse(out), nil
}
