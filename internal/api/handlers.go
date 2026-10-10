package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/health"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/search"
)

// RealtimeConnTracker tracks active real-time websocket clients.
type RealtimeConnTracker interface {
	Connections() int
}

// GRPCConnTracker tracks active gRPC client connections.
type GRPCConnTracker interface {
	ActiveConnections() int
}

// SessionConnTracker tracks active media streaming sessions.
type SessionConnTracker interface {
	ActiveCount() int
}

// Handlers implements gen.StrictServerInterface. Each module adds its methods in its own file.
type Handlers struct {
	Inv             *inventory.Service
	Provision       *provision.Service
	AgentEnroll     *agentenroll.Service
	Events          *events.Service
	Alarms          *alarms.Service
	Identity        *identity.Service
	Media           *media.Service
	Branding        *branding.Service
	ClipWatermark   *clipwatermark.Service
	Search          *search.Service
	Rules           *rules.Service
	Notify          *notify.Service
	Maps            *maps.Service
	Features        config.Features
	Log             *slog.Logger
	WorkerURL       string
	Checks          []health.Check
	CheckTimeout    time.Duration
	SchemaVersion   func(ctx context.Context) (int64, error)
	RealtimeTracker RealtimeConnTracker
	GRPCTracker     GRPCConnTracker
	SessionsTracker SessionConnTracker
}

var _ gen.StrictServerInterface = (*Handlers)(nil)

func (h *Handlers) GetLiveness(context.Context, gen.GetLivenessRequestObject) (gen.GetLivenessResponseObject, error) {
	return gen.GetLiveness200JSONResponse{Status: gen.LivenessStatusStatusOk}, nil
}

func (h *Handlers) GetReadiness(ctx context.Context, _ gen.GetReadinessRequestObject) (gen.GetReadinessResponseObject, error) {
	results := health.Run(ctx, h.Checks, h.CheckTimeout)
	body := gen.ReadinessStatus{Status: gen.ReadinessStatusStatusOk, Checks: make([]gen.DependencyCheck, 0, len(results))}
	for _, r := range results {
		c := gen.DependencyCheck{Name: r.Name, Status: gen.DependencyCheckStatusOk, LatencyMs: r.Latency.Milliseconds()}
		if r.Err != nil {
			msg := r.Err.Error()
			c.Status, c.Error = gen.DependencyCheckStatusError, &msg
			h.Log.WarnContext(ctx, "readiness check failed", "dependency", r.Name, "error", r.Err)
		}
		body.Checks = append(body.Checks, c)
	}
	if !health.Healthy(results) {
		body.Status = gen.ReadinessStatusStatusDegraded
		return gen.GetReadiness503JSONResponse(body), nil
	}
	return gen.GetReadiness200JSONResponse(body), nil
}

func (h *Handlers) GetSystemInfo(ctx context.Context, _ gen.GetSystemInfoRequestObject) (gen.GetSystemInfoResponseObject, error) {
	version, err := h.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetSystemInfo200JSONResponse{
		Name:          "openvms-api",
		Version:       buildinfo.Version,
		Commit:        buildinfo.Commit,
		BuildTime:     buildinfo.BuildTime,
		GoVersion:     buildinfo.GoVersion(),
		SchemaVersion: version,
	}, nil
}
