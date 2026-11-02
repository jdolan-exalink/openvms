package main

import (
	"encoding/json"
	"net/http"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/backup"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

// mountOnvifOps registers the camera operations the control plane drives through the agent (video,
// PTZ, imaging, reboot, snapshot, time sync, event pull). It serves both listeners: an installed agent
// is reached over the TLS one, and a route missing there answers 404, which the control plane reads
// as "the agent is too old" (it then refuses to configure a camera's video, for one).
func mountOnvifOps(mux *http.ServeMux, auth *authenticator, ops *onvifdiscover.OpsHandler) {
	if ops == nil {
		return
	}
	control := func(h http.HandlerFunc) http.Handler { return auth.scoped(tokens.ScopeControl, h) }
	mux.Handle("POST /v1/onvif/sync-time", control(ops.HandleSyncTime))
	mux.Handle("POST /v1/onvif/ptz", control(ops.HandlePTZ))
	mux.Handle("POST /v1/onvif/video-config", control(ops.HandleVideoConfig))
	mux.Handle("POST /v1/onvif/video-encoding", control(ops.HandleVideoEncoding))
	mux.Handle("POST /v1/onvif/video-device", control(ops.HandleVideoDevice))
	mux.Handle("POST /v1/onvif/reboot", control(ops.HandleReboot))
	mux.Handle("POST /v1/onvif/imaging", control(ops.HandleImaging))
	mux.Handle("POST /v1/onvif/snapshot", control(ops.HandleSnapshot))
	mux.Handle("POST /v1/onvif/pull-events", control(ops.HandlePullEvents))
}

// mountProvisionLPRConfigure registers the route that pushes the LPR settings to a Provision-ISR
// camera. Like the ONVIF operations it must answer on every listener.
func mountProvisionLPRConfigure(mux *http.ServeMux, auth *authenticator) {
	configurator := provisionlpr.NewConfigurator()
	mux.Handle("POST /v1/provision-lpr/configure", auth.scoped(tokens.ScopeControl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p provisionlpr.CameraConfigParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		res := configurator.Configure(r.Context(), p)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})))
}

// agentCore is what an installed agent offers besides the camera operations: its status, replacing
// its own binary and the original Frigate configuration backup. The control plane reaches an installed
// agent over the TLS listener, so both listeners mount it; a route missing there answers 404 and the
// control plane reads that as an unreachable or outdated agent.
type agentCore struct {
	variant string
	sampler *agent.Sampler
	replace func(*http.Request) error
	restart func()
	backup  *backup.Store
}

func mountAgentCore(mux *http.ServeMux, auth *authenticator, core *agentCore) {
	if core == nil {
		return
	}
	if core.sampler != nil {
		mux.Handle("GET /v1/metrics", auth.scoped(tokens.ScopeRead, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, core.sampler.Current(core.variant))
		})))
	}
	if core.replace != nil && core.restart != nil {
		mux.Handle("POST /v1/update", auth.scoped(tokens.ScopeUpdate, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := core.replace(r); err != nil {
				http.Error(w, err.Error(), updateStatus(err))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			core.restart()
		})))
	}
	if core.backup != nil {
		mountBackup(mux, auth, core.backup)
	}
}
