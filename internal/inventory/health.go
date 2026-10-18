package inventory

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// StatusChange is emitted when a server's health status changes.
type StatusChange struct {
	ServerID uuid.UUID `json:"server_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Error    string    `json:"error,omitempty"`
	At       time.Time `json:"at"`
}

// CameraStatusChange is emitted when a camera's health status changes.
type CameraStatusChange struct {
	CameraID uuid.UUID `json:"camera_id"`
	ServerID uuid.UUID `json:"server_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	SiteID   uuid.UUID `json:"site_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

// HealthPoller checks every registered Frigate server on an interval (PRD §59-60).
type HealthPoller struct {
	Svc            *Service
	Interval       time.Duration
	Concurrency    int
	OnChange       func(ctx context.Context, c StatusChange)
	OnCameraChange func(ctx context.Context, c CameraStatusChange)

	// CapabilityInterval is how often each server's capability flags (face recognition, LPR...)
	// are re-read from Frigate; zero means DefaultCapabilityInterval. A flag that changes on the
	// Frigate side (enabled in config.yml and restarted) is picked up here.
	CapabilityInterval time.Duration

	capMu        sync.Mutex
	capRefreshed map[uuid.UUID]time.Time

	// Adapters is shared with the event syncer; nil creates a private pool.
	Adapters *Adapters
}

// DefaultCapabilityInterval is the capability refresh period of the health poller.
const DefaultCapabilityInterval = 2 * time.Minute

// Storage above this share of used space marks a server degraded (PRD §61 "Disk > 90%").
const storageDegradedRatio = 0.9

func (p *HealthPoller) Run(ctx context.Context) {
	p.PollOnce(ctx)
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.PollOnce(ctx)
		}
	}
}

func (p *HealthPoller) PollOnce(ctx context.Context) {
	if p.Adapters == nil {
		p.Adapters = NewAdapters(p.Svc)
	}
	var servers []db.FrigateServer
	err := p.Svc.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		servers, err = q.ListAllActiveServers(ctx)
		return err
	})
	if err != nil {
		p.Svc.Log.ErrorContext(ctx, "health: list servers", "error", err)
		return
	}
	sem := make(chan struct{}, max(p.Concurrency, 1))
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			p.check(cctx, srv)
		}()
	}
	wg.Wait()
}

func (p *HealthPoller) check(ctx context.Context, srv db.FrigateServer) {
	status, errMsg := "online", ""
	var stats frigate.Stats
	a, err := p.Adapters.Get(ctx, srv)
	if err == nil {
		stats, err = a.Stats(ctx)
	}
	if err != nil {
		status, errMsg = "offline", err.Error()
		p.Adapters.Forget(srv.ID) // reconnect (and re-detect version) next time
	}

	var camChanges []CameraStatusChange
	now := time.Now().UTC()

	err = p.Svc.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		if status == "offline" {
			if srv.Status != "offline" {
				cams, err := q.ListServerCameras(ctx, srv.ID)
				if err == nil {
					for _, cam := range cams {
						if cam.Status != "unknown" {
							camChanges = append(camChanges, CameraStatusChange{
								CameraID: cam.ID,
								ServerID: srv.ID,
								TenantID: srv.TenantID,
								SiteID:   srv.SiteID,
								From:     cam.Status,
								To:       "unknown",
								At:       now,
							})
						}
					}
				}
			}
			if err := q.SetServerCamerasStatus(ctx, db.SetServerCamerasStatusParams{ServerID: srv.ID, Status: "unknown"}); err != nil {
				return err
			}
			return q.UpdateServerHealth(ctx, db.UpdateServerHealthParams{ID: srv.ID, Status: status, Reachable: false, LastError: errMsg, Stats: srv.Stats})
		}
		cams, err := q.ListServerCameras(ctx, srv.ID)
		if err != nil {
			return err
		}
		degraded := false
		for _, cam := range cams {
			cs, reported := stats.Cameras[cam.RemoteName]
			camStatus := "offline"
			switch {
			case !cam.Enabled:
				camStatus = "unknown"
			case reported && cs.CameraFPS > 0:
				camStatus = "online"
			}
			if cam.Enabled && camStatus == "offline" && cam.MissingSince == nil {
				degraded = true
			}
			var fps *float32
			if reported {
				fps = ptr(float32(cs.CameraFPS))
			}
			if err := q.UpdateCameraHealth(ctx, db.UpdateCameraHealthParams{ServerID: srv.ID, RemoteName: cam.RemoteName, Status: camStatus, Fps: fps}); err != nil {
				return err
			}
			if camStatus != cam.Status {
				camChanges = append(camChanges, CameraStatusChange{
					CameraID: cam.ID,
					ServerID: srv.ID,
					TenantID: srv.TenantID,
					SiteID:   srv.SiteID,
					From:     cam.Status,
					To:       camStatus,
					At:       now,
				})
			}
		}
		if r := stats.Recordings; r != nil && r.TotalMB > 0 && r.UsedMB/r.TotalMB > storageDegradedRatio {
			degraded = true
		}
		if degraded {
			status = "degraded"
		}
		raw, _ := json.Marshal(HealthStats{UptimeSeconds: stats.UptimeSeconds, Recordings: stats.Recordings})
		return q.UpdateServerHealth(ctx, db.UpdateServerHealthParams{
			ID: srv.ID, Status: status, Reachable: true, LastError: "", Stats: raw, FrigateVersion: stats.Version,
		})
	})
	if err != nil {
		p.Svc.Log.ErrorContext(ctx, "health: store result", "server_id", srv.ID, "error", err)
		return
	}
	if status != "offline" && a != nil {
		p.refreshCapabilities(ctx, srv, a)
	}
	if status != srv.Status && p.OnChange != nil {
		p.OnChange(ctx, StatusChange{ServerID: srv.ID, TenantID: srv.TenantID, SiteID: srv.SiteID, From: srv.Status, To: status, Error: errMsg, At: now})
	}
	if p.OnCameraChange != nil {
		for _, c := range camChanges {
			p.OnCameraChange(ctx, c)
		}
	}
}

// refreshCapabilities re-reads the capability flags of an online server at most once per
// CapabilityInterval and stores them when they changed.
func (p *HealthPoller) refreshCapabilities(ctx context.Context, srv db.FrigateServer, a frigate.Adapter) {
	every := p.CapabilityInterval
	if every <= 0 {
		every = DefaultCapabilityInterval
	}
	p.capMu.Lock()
	if p.capRefreshed == nil {
		p.capRefreshed = map[uuid.UUID]time.Time{}
	}
	if last, ok := p.capRefreshed[srv.ID]; ok && time.Since(last) < every {
		p.capMu.Unlock()
		return
	}
	p.capRefreshed[srv.ID] = time.Now()
	p.capMu.Unlock()
	if err := p.Svc.refreshCapabilities(ctx, srv.ID, func(context.Context) (frigate.Adapter, error) { return a, nil }); err != nil {
		p.Svc.Log.WarnContext(ctx, "health: capability refresh failed", "server_id", srv.ID, "error", err)
	}
}
