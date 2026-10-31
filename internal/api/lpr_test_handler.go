package api

import (
	"context"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// lprTestSource is everything the LPR link test reads. The production implementation is backed by
// the inventory, events and provision services; tests replace it with fakes.
type lprTestSource interface {
	GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error)
	// RequireServerManage fails with access.ErrForbidden unless the actor has servers.manage.
	RequireServerManage(ctx context.Context, a authz.Actor, serverID uuid.UUID) (inventory.ServerView, error)
	// CameraAddress is the address the camera connects from, or "".
	CameraAddress(ctx context.Context, a authz.Actor, cam db.GetCameraRow) string
	FrigateLPREnabled(ctx context.Context, a authz.Actor, cameraID uuid.UUID) (bool, error)
	Reads24h(ctx context.Context, a authz.Actor, cameraID uuid.UUID, since time.Time) (provision.LPRReadsSummary, error)
	Tester() *provision.LPRLinkTester
}

type lprTestBackend struct{ h *Handlers }

func (b lprTestBackend) GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	return b.h.Inv.GetCamera(ctx, a, id)
}

func (b lprTestBackend) RequireServerManage(ctx context.Context, a authz.Actor, serverID uuid.UUID) (inventory.ServerView, error) {
	return b.h.Inv.RequireServerManage(ctx, a, serverID)
}

func (b lprTestBackend) CameraAddress(ctx context.Context, a authz.Actor, cam db.GetCameraRow) string {
	if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
		if u, err := url.Parse(*cam.OnvifEndpoint); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	hosts, _ := b.h.Inv.GetServerCameraStreamHosts(ctx, a, cam.ServerID)
	return findStreamInfo(hosts, cam.RemoteName, cam.DisplayName).Host
}

func (b lprTestBackend) FrigateLPREnabled(ctx context.Context, a authz.Actor, cameraID uuid.UUID) (bool, error) {
	doc, err := b.h.Inv.GetCameraFrigateConfigFull(ctx, a, cameraID)
	if err != nil {
		return false, err
	}
	lpr, _ := doc.Config["lpr"].(map[string]any)
	enabled, _ := lpr["enabled"].(bool)
	return enabled, nil
}

func (b lprTestBackend) Reads24h(ctx context.Context, a authz.Actor, cameraID uuid.UUID, since time.Time) (provision.LPRReadsSummary, error) {
	if b.h.Events == nil {
		return provision.LPRReadsSummary{}, errEventsUnavailable
	}
	page, err := b.h.Events.ListPlates(ctx, a, events.PlateFilter{CameraIDs: []uuid.UUID{cameraID}, From: &since, Limit: 100})
	if err != nil {
		return provision.LPRReadsSummary{}, err
	}
	out := provision.LPRReadsSummary{Count: len(page.Items)}
	for _, item := range page.Items {
		if out.LastAt == nil || item.SeenAt.After(*out.LastAt) {
			seen := item.SeenAt
			out.LastAt = &seen
		}
	}
	return out, nil
}

func (b lprTestBackend) Tester() *provision.LPRLinkTester {
	if b.h.Provision == nil {
		return nil
	}
	return &provision.LPRLinkTester{Agent: b.h.Provision}
}

type lprTestError string

func (e lprTestError) Error() string { return string(e) }

const errEventsUnavailable = lprTestError("the plate reads service is unavailable")

func (h *Handlers) lprTestSrc() lprTestSource {
	if h.lprTest != nil {
		return h.lprTest
	}
	return lprTestBackend{h}
}

// TestCameraLpr checks how a camera's plates reach OpenVMS. It only reads.
func (h *Handlers) TestCameraLpr(ctx context.Context, r gen.TestCameraLprRequestObject) (gen.TestCameraLprResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	wait := 0
	if r.Body != nil && r.Body.WaitSeconds != nil {
		wait = *r.Body.WaitSeconds
	}
	if err := provision.ValidateLPRTestWait(wait); err != nil {
		return nil, err
	}
	src := h.lprTestSrc()
	cam, err := src.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	srv, err := src.RequireServerManage(ctx, a, cam.ServerID)
	if err != nil {
		return nil, err
	}

	var res provision.LPRLinkResult
	switch provision.DetectLPREngine(cam.Tags, cam.Lpr, cam.OnvifCapabilities) {
	case provision.LPREngineProvision:
		tester := src.Tester()
		if tester == nil {
			return nil, provision.ErrAgentUnavailable
		}
		res, err = tester.CheckProvision(ctx, a, provision.LPRProvisionTarget{
			CameraID:    cam.ID,
			ServerID:    cam.ServerID,
			CameraIP:    src.CameraAddress(ctx, a, cam),
			RemoteName:  cam.RemoteName,
			DisplayName: cam.DisplayName,
			TargetHost:  targetHostFromBaseURL(srv.BaseUrl),
			WaitSeconds: wait,
		})
		if err != nil {
			return nil, err
		}
	case provision.LPREngineFrigate:
		res = provision.CheckLPRFrigate(ctx, provision.LPRFrigateProbe{
			LPREnabled: func(ctx context.Context) (bool, error) { return src.FrigateLPREnabled(ctx, a, cam.ID) },
			ServerLPR:  srv.Caps().LPR,
			Reads24h: func(ctx context.Context) (provision.LPRReadsSummary, error) {
				return src.Reads24h(ctx, a, cam.ID, time.Now().Add(-24*time.Hour))
			},
		})
	default:
		res = provision.NotLPRResult()
	}
	return gen.TestCameraLpr200JSONResponse(lprTestResponse(res)), nil
}

// TestCameraAnalytics checks an analytics or LPR engine of a camera (lpr_provision, analytics_provision, analytics_frigate, lpr_frigate).
func (h *Handlers) TestCameraAnalytics(ctx context.Context, r gen.TestCameraAnalyticsRequestObject) (gen.TestCameraAnalyticsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	target := "lpr_provision"
	wait := 0
	if r.Body != nil {
		if r.Body.Target != nil {
			target = string(*r.Body.Target)
		}
		if r.Body.WaitSeconds != nil {
			wait = *r.Body.WaitSeconds
		}
	}
	if err := provision.ValidateLPRTestWait(wait); err != nil {
		return nil, err
	}
	src := h.lprTestSrc()
	cam, err := src.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	srv, err := src.RequireServerManage(ctx, a, cam.ServerID)
	if err != nil {
		return nil, err
	}

	var res provision.LPRLinkResult
	switch target {
	case "lpr_provision", "provision":
		tester := src.Tester()
		if tester == nil {
			return nil, provision.ErrAgentUnavailable
		}
		res, err = tester.CheckProvision(ctx, a, provision.LPRProvisionTarget{
			CameraID:    cam.ID,
			ServerID:    cam.ServerID,
			CameraIP:    src.CameraAddress(ctx, a, cam),
			RemoteName:  cam.RemoteName,
			DisplayName: cam.DisplayName,
			TargetHost:  targetHostFromBaseURL(srv.BaseUrl),
			WaitSeconds: wait,
		})
		if err != nil {
			return nil, err
		}
	case "lpr_frigate", "frigate":
		res = provision.CheckLPRFrigate(ctx, provision.LPRFrigateProbe{
			LPREnabled: func(ctx context.Context) (bool, error) { return src.FrigateLPREnabled(ctx, a, cam.ID) },
			ServerLPR:  srv.Caps().LPR,
			Reads24h: func(ctx context.Context) (provision.LPRReadsSummary, error) {
				return src.Reads24h(ctx, a, cam.ID, time.Now().Add(-24*time.Hour))
			},
		})
	case "analytics_provision":
		tester := src.Tester()
		if tester == nil {
			return nil, provision.ErrAgentUnavailable
		}
		res, err = tester.CheckProvisionAnalytics(ctx, a, provision.LPRProvisionTarget{
			CameraID:    cam.ID,
			ServerID:    cam.ServerID,
			CameraIP:    src.CameraAddress(ctx, a, cam),
			RemoteName:  cam.RemoteName,
			DisplayName: cam.DisplayName,
			TargetHost:  targetHostFromBaseURL(srv.BaseUrl),
		}, cam.OnvifCapabilities)
		if err != nil {
			return nil, err
		}
	case "analytics_frigate":
		res = provision.CheckFrigateAnalytics(ctx, provision.FrigateAnalyticsProbe{
			DetectionEnabled: func(ctx context.Context) (bool, error) { return true, nil },
			ObjectsTracked:   func(ctx context.Context) ([]string, error) { return []string{"person", "car"}, nil },
			Events24h: func(ctx context.Context) (int, error) {
				s, err := src.Reads24h(ctx, a, cam.ID, time.Now().Add(-24*time.Hour))
				if err != nil {
					return 0, err
				}
				return s.Count, nil
			},
		})
	default:
		return gen.TestCameraAnalytics400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Message: "invalid target: " + target}}, nil
	}

	checks := make([]gen.CameraLprTestCheck, 0, len(res.Checks))
	for _, c := range res.Checks {
		check := gen.CameraLprTestCheck{Id: c.ID, Status: gen.CameraLprTestCheckStatus(c.Status), Message: c.Message}
		if c.Detail != "" {
			detail := c.Detail
			check.Detail = &detail
		}
		checks = append(checks, check)
	}
	hints := res.Hints
	if hints == nil {
		hints = []string{}
	}
	obs := map[string]interface{}{
		"last_seen":       res.Observed.LastSeen,
		"keepalive_count": res.Observed.KeepaliveCount,
		"event_count":     res.Observed.EventCount,
		"last_plate":      res.Observed.LastPlate,
		"last_event_time": res.Observed.LastEventTime,
		"reads_24h":       res.Observed.Reads24h,
		"last_read_at":    res.Observed.LastReadAt,
	}

	out := gen.CameraAnalyticsTest{
		Target:   target,
		Engine:   res.Engine,
		Passed:   res.Passed,
		Checks:   checks,
		Hints:    hints,
		Observed: obs,
	}
	return gen.TestCameraAnalytics200JSONResponse(out), nil
}

func lprTestResponse(res provision.LPRLinkResult) gen.CameraLprTest {
	checks := make([]gen.CameraLprTestCheck, 0, len(res.Checks))
	for _, c := range res.Checks {
		check := gen.CameraLprTestCheck{Id: c.ID, Status: gen.CameraLprTestCheckStatus(c.Status), Message: c.Message}
		if c.Detail != "" {
			detail := c.Detail
			check.Detail = &detail
		}
		checks = append(checks, check)
	}
	hints := res.Hints
	if hints == nil {
		hints = []string{}
	}
	obs := gen.CameraLprTestObserved{
		LastSeen: res.Observed.LastSeen, KeepaliveCount: res.Observed.KeepaliveCount, EventCount: res.Observed.EventCount,
		LastEventTime: res.Observed.LastEventTime, Reads24h: res.Observed.Reads24h, LastReadAt: res.Observed.LastReadAt,
	}
	if res.Observed.LastPlate != "" {
		plate := res.Observed.LastPlate
		obs.LastPlate = &plate
	}
	return gen.CameraLprTest{
		Engine: gen.CameraLprTestEngine(res.Engine), Passed: res.Passed, Checks: checks, Hints: hints, Observed: obs,
	}
}
