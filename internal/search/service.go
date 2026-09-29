package search

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var (
	ErrQueryTooShort = errors.New("query must be at least 2 characters")
)

type Service struct {
	Store *store.Store
}

type CameraHit struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	SiteID      uuid.UUID `json:"site_id"`
	SiteName    string    `json:"site_name"`
	Location    string    `json:"location"`
	Status      string    `json:"status"`
}

type SiteHit struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	CameraCount int       `json:"camera_count"`
}

type ServerHit struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	SiteID   uuid.UUID `json:"site_id"`
	SiteName string    `json:"site_name"`
	Status   string    `json:"status"`
}

type EventHit struct {
	ID           uuid.UUID `json:"id"`
	CameraID     uuid.UUID `json:"camera_id"`
	CameraName   string    `json:"camera_name"`
	SiteName     string    `json:"site_name"`
	Severity     string    `json:"severity"`
	Labels       []string  `json:"labels"`
	StartTime    time.Time `json:"start_time"`
	HasThumbnail bool      `json:"has_thumbnail"`
}

type PlateHit struct {
	Plate      string     `json:"plate"`
	CameraID   uuid.UUID  `json:"camera_id"`
	CameraName string     `json:"camera_name"`
	SiteName   string     `json:"site_name"`
	SeenAt     time.Time  `json:"seen_at"`
	EventID    *uuid.UUID `json:"event_id,omitempty"`
}

type Result struct {
	Cameras []CameraHit `json:"cameras"`
	Sites   []SiteHit   `json:"sites"`
	Servers []ServerHit `json:"servers"`
	Events  []EventHit  `json:"events"`
	Plates  []PlateHit  `json:"plates"`
}

func (s *Service) Search(ctx context.Context, actor authz.Actor, rawQuery string) (Result, error) {
	q := strings.TrimSpace(rawQuery)
	if len([]rune(q)) < 2 {
		return Result{}, ErrQueryTooShort
	}

	var res Result
	res.Cameras = []CameraHit{}
	res.Sites = []SiteHit{}
	res.Servers = []ServerHit{}
	res.Events = []EventHit{}
	res.Plates = []PlateHit{}

	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(queries *db.Queries) error {
		chk, err := access.Load(ctx, queries, actor)
		if err != nil {
			return err
		}

		// 1. Cameras: caller needs cameras.view
		camIDs, err := chk.CameraIDs(ctx, authz.CamerasView)
		if err != nil {
			return err
		}
		if len(camIDs) > 0 {
			camRows, err := queries.SearchCameras(ctx, db.SearchCamerasParams{
				CameraIds: camIDs,
				Query:     q,
			})
			if err != nil {
				return store.Classify(err)
			}
			res.Cameras = make([]CameraHit, len(camRows))
			for i, r := range camRows {
				res.Cameras[i] = CameraHit{
					ID:          r.ID,
					DisplayName: r.DisplayName,
					SiteID:      r.SiteID,
					SiteName:    r.SiteName,
					Location:    r.Location,
					Status:      r.Status,
				}
			}
		}

		// 2. Sites: caller needs sites.view
		siteIDs, err := chk.SiteIDs(ctx, authz.SitesView)
		if err != nil {
			return err
		}
		if len(siteIDs) > 0 {
			siteRows, err := queries.SearchSites(ctx, db.SearchSitesParams{
				SiteIds: siteIDs,
				Query:   q,
			})
			if err != nil {
				return store.Classify(err)
			}
			res.Sites = make([]SiteHit, len(siteRows))
			for i, r := range siteRows {
				res.Sites[i] = SiteHit{
					ID:          r.ID,
					Name:        r.Name,
					CameraCount: int(r.CameraCount),
				}
			}
		}

		// 3. Servers: caller needs servers.view
		serverIDs, err := chk.ServerIDs(ctx, authz.ServersView)
		if err != nil {
			return err
		}
		if len(serverIDs) > 0 {
			serverRows, err := queries.SearchServers(ctx, db.SearchServersParams{
				ServerIds: serverIDs,
				Query:     q,
			})
			if err != nil {
				return store.Classify(err)
			}
			res.Servers = make([]ServerHit, len(serverRows))
			for i, r := range serverRows {
				res.Servers[i] = ServerHit{
					ID:       r.ID,
					Name:     r.Name,
					SiteID:   r.SiteID,
					SiteName: r.SiteName,
					Status:   r.Status,
				}
			}
		}

		// 4. Events: caller needs events.search
		eventCamIDs, err := chk.CameraIDs(ctx, authz.EventsSearch)
		if err != nil {
			return err
		}
		if len(eventCamIDs) > 0 {
			eventRows, err := queries.SearchEvents(ctx, db.SearchEventsParams{
				CameraIds: eventCamIDs,
				Query:     q,
			})
			if err != nil {
				return store.Classify(err)
			}
			res.Events = make([]EventHit, len(eventRows))
			for i, r := range eventRows {
				res.Events[i] = EventHit{
					ID:           r.ID,
					CameraID:     r.CameraID,
					CameraName:   r.CameraName,
					SiteName:     r.SiteName,
					Severity:     r.Severity,
					Labels:       r.Labels,
					StartTime:    r.StartTime,
					HasThumbnail: r.HasThumbnail,
				}
			}
		}

		// 5. Plates: caller needs lpr.search
		lprCamIDs, err := chk.CameraIDs(ctx, authz.LPRSearch)
		if err != nil {
			return err
		}
		if len(lprCamIDs) > 0 {
			plateRows, err := queries.SearchPlates(ctx, db.SearchPlatesParams{
				CameraIds: lprCamIDs,
				Query:     q,
			})
			if err != nil {
				return store.Classify(err)
			}
			res.Plates = make([]PlateHit, len(plateRows))
			for i, r := range plateRows {
				var evID *uuid.UUID
				if r.EventID != nil {
					u := *r.EventID
					evID = &u
				}
				res.Plates[i] = PlateHit{
					Plate:      r.Plate,
					CameraID:   r.CameraID,
					CameraName: r.CameraName,
					SiteName:   r.SiteName,
					SeenAt:     r.SeenAt,
					EventID:    evID,
				}
			}
		}

		return nil
	})

	return res, err
}
