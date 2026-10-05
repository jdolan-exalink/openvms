package maps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// GetAnalytics calculates activity heatmap points and totals according to metric, timeframe, and filters.
func (s *Service) GetAnalytics(ctx context.Context, actor authz.Actor, query AnalyticsQuery) (*AnalyticsResult, error) {
	// Normalize timeframe
	if query.End.IsZero() {
		query.End = time.Now().UTC()
	}
	if query.Start.IsZero() {
		query.Start = query.End.Add(-24 * time.Hour)
	}
	if query.Start.After(query.End) {
		query.Start, query.End = query.End, query.Start
	}

	// Normalize metric
	if query.Metric == "" {
		query.Metric = MetricObject
	}
	switch query.Metric {
	case MetricObject, MetricPerson, MetricVehicle, MetricMotion, MetricAlarm, MetricLPR:
	default:
		return nil, &ValidationError{Msg: fmt.Sprintf("invalid analytics metric: %s", query.Metric)}
	}

	// Determine required camera permission
	var reqPerm authz.Permission
	switch query.Metric {
	case MetricAlarm:
		reqPerm = authz.AlarmsView
	case MetricLPR:
		reqPerm = authz.LPRSearch
	default:
		reqPerm = authz.EventsView
	}

	var res AnalyticsResult
	res.Points = []AnalyticsPoint{}

	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}

		// Resolve site and permissions
		var siteID uuid.UUID
		if query.SiteID != nil {
			siteID = *query.SiteID
			site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
			if err != nil {
				return store.Classify(err)
			}
			if err := chk.Require(authz.MapsView, access.Site(site.TenantID, siteID)); err != nil {
				return err
			}
		} else if query.CameraID != nil {
			// Find site for this camera
			placement, err := q.GetMapPlacementByEntityGeo(ctx, db.GetMapPlacementByEntityGeoParams{
				EntityType: "camera",
				EntityID:   *query.CameraID,
				TenantID:   actor.TenantID,
			})
			if err != nil {
				return store.Classify(err)
			}
			siteID = placement.SiteID
			if err := chk.Require(authz.MapsView, access.Site(placement.TenantID, siteID)); err != nil {
				return err
			}
		} else {
			return &ValidationError{Msg: "site_id or camera_id is required"}
		}

		// Determine camera access
		canViewAllCameras := actor.IsPlatform() ||
			(actor.TenantID != nil && chk.Can(reqPerm, access.Tenant(*actor.TenantID))) ||
			chk.Can(reqPerm, access.Site(*actor.TenantID, siteID))

		var allowedCameraIDs map[uuid.UUID]bool
		if !canViewAllCameras {
			cams, err := chk.CameraIDs(ctx, reqPerm)
			if err != nil {
				return err
			}
			if len(cams) == 0 {
				return nil
			}
			allowedCameraIDs = make(map[uuid.UUID]bool, len(cams))
			for _, id := range cams {
				allowedCameraIDs[id] = true
			}
		}

		// Load placed cameras on the site
		isGeo := true
		placements, err := q.ListMapPlacementsDetailed(ctx, db.ListMapPlacementsDetailedParams{
			SiteID:   siteID,
			TenantID: actor.TenantID,
			IsGeo:    &isGeo,
		})
		if err != nil {
			return store.Classify(err)
		}

		// If a zone is specified, load and parse its polygon
		var zonePoints []Point
		if query.ZoneID != nil {
			zone, err := q.GetMapZone(ctx, db.GetMapZoneParams{
				ID:       *query.ZoneID,
				TenantID: actor.TenantID,
			})
			if err != nil {
				return store.Classify(err)
			}
			pts, _, err := ValidatePolygon(zone.Geometry, zone.FloorID != nil)
			if err != nil {
				return err
			}
			zonePoints = pts
		}

		type placedCam struct {
			id         uuid.UUID
			lat        float64
			lng        float64
			bearingDeg float64
			rangeM     float64
		}

		candidateCameras := make(map[uuid.UUID]placedCam)
		candidateIDs := make([]uuid.UUID, 0, len(placements))

		for _, p := range placements {
			if p.EntityType != "camera" {
				continue
			}
			if query.CameraID != nil && p.EntityID != *query.CameraID {
				continue
			}
			if !canViewAllCameras && !allowedCameraIDs[p.EntityID] {
				continue
			}
			if p.Lat == nil || p.Lng == nil {
				continue
			}

			lat := *p.Lat
			lng := *p.Lng

			// Zone boundary check
			if len(zonePoints) > 0 {
				if !PointInPolygon(Point{X: lng, Y: lat}, zonePoints) {
					continue
				}
			}

			var bearing, rangeM float64
			if p.BearingDeg != nil {
				bearing = float64(*p.BearingDeg)
			}
			if p.RangeM != nil && *p.RangeM > 0 {
				rangeM = float64(*p.RangeM)
			} else {
				rangeM = 30.0
			}

			candidateCameras[p.EntityID] = placedCam{
				id:         p.EntityID,
				lat:        lat,
				lng:        lng,
				bearingDeg: bearing,
				rangeM:     rangeM,
			}
			candidateIDs = append(candidateIDs, p.EntityID)
		}

		if len(candidateIDs) == 0 {
			return nil
		}

		// Query counts based on metric and timeframe
		duration := query.End.Sub(query.Start)
		counts := make(map[uuid.UUID]int64)

		switch query.Metric {
		case MetricAlarm:
			rows, err := q.QueryMapAnalyticsAlarms(ctx, db.QueryMapAnalyticsAlarmsParams{
				TenantID:  actor.TenantID,
				StartTime: query.Start,
				EndTime:   query.End,
				CameraIds: candidateIDs,
			})
			if err != nil {
				return store.Classify(err)
			}
			for _, r := range rows {
				counts[r.CameraID] = r.Count
			}

		case MetricLPR:
			rows, err := q.QueryMapAnalyticsLPR(ctx, db.QueryMapAnalyticsLPRParams{
				TenantID:  actor.TenantID,
				StartTime: query.Start,
				EndTime:   query.End,
				CameraIds: candidateIDs,
			})
			if err != nil {
				return store.Classify(err)
			}
			for _, r := range rows {
				counts[r.CameraID] = r.Count
			}

		default:
			var labels []string
			if query.ObjectType != nil && strings.TrimSpace(*query.ObjectType) != "" {
				labels = []string{strings.TrimSpace(*query.ObjectType)}
			} else {
				switch query.Metric {
				case MetricPerson:
					labels = []string{"person"}
				case MetricVehicle:
					labels = []string{"car", "motorcycle", "bus", "truck", "vehicle"}
				case MetricMotion:
					labels = []string{"motion"}
				}
			}

			if duration <= 24*time.Hour {
				rows, err := q.QueryMapAnalyticsEvents(ctx, db.QueryMapAnalyticsEventsParams{
					TenantID:  actor.TenantID,
					StartTime: query.Start,
					EndTime:   query.End,
					CameraIds: candidateIDs,
					Labels:    labels,
				})
				if err != nil {
					return store.Classify(err)
				}
				for _, r := range rows {
					counts[r.CameraID] = r.Count
				}
			} else {
				rows, err := q.QueryMapAnalyticsRollups(ctx, db.QueryMapAnalyticsRollupsParams{
					TenantID:  actor.TenantID,
					StartTime: query.Start,
					EndTime:   query.End,
					CameraIds: candidateIDs,
					Labels:    labels,
				})
				if err != nil {
					return store.Classify(err)
				}
				for _, r := range rows {
					counts[r.CameraID] = r.Count
				}
			}
		}

		// Calculate total and maxCount
		var total int64
		var maxCount int64
		for _, count := range counts {
			total += count
			if count > maxCount {
				maxCount = count
			}
		}

		res.Total = total
		res.MaxCount = maxCount

		if maxCount == 0 {
			return nil
		}

		// Build weighted points
		for camID, count := range counts {
			if count <= 0 {
				continue
			}
			cam, ok := candidateCameras[camID]
			if !ok {
				continue
			}

			if query.Coverage {
				// Spread across 3 points along the FOV axis: camera, 50% range, 100% range
				weightPerPoint := (float64(count) / float64(maxCount)) / 3.0
				countPerPoint := count / 3
				if countPerPoint < 1 {
					countPerPoint = 1
				}

				// Point 0: at camera
				res.Points = append(res.Points, AnalyticsPoint{
					CameraID: &cam.id,
					Lat:      cam.lat,
					Lng:      cam.lng,
					Weight:   weightPerPoint,
					Count:    countPerPoint,
				})

				// Point 1: at 50% range
				lat50, lng50 := DestinationPoint(cam.lat, cam.lng, cam.bearingDeg, cam.rangeM*0.5)
				res.Points = append(res.Points, AnalyticsPoint{
					CameraID: &cam.id,
					Lat:      lat50,
					Lng:      lng50,
					Weight:   weightPerPoint,
					Count:    countPerPoint,
				})

				// Point 2: at 100% range
				lat100, lng100 := DestinationPoint(cam.lat, cam.lng, cam.bearingDeg, cam.rangeM*1.0)
				res.Points = append(res.Points, AnalyticsPoint{
					CameraID: &cam.id,
					Lat:      lat100,
					Lng:      lng100,
					Weight:   weightPerPoint,
					Count:    countPerPoint,
				})
			} else {
				weight := float64(count) / float64(maxCount)
				res.Points = append(res.Points, AnalyticsPoint{
					CameraID: &cam.id,
					Lat:      cam.lat,
					Lng:      cam.lng,
					Weight:   weight,
					Count:    count,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &res, nil
}
