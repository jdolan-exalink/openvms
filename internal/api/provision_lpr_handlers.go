package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

type configureProvisionLPRBody struct {
	CameraIP       string `json:"camera_ip,omitempty"`
	CameraPort     int    `json:"camera_port,omitempty"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetPort     int    `json:"target_port,omitempty"`
	KeepalivePath  string `json:"keepalive_path,omitempty"`
	EventPath      string `json:"event_path,omitempty"`
	IntervalSecond int    `json:"interval_second,omitempty"`
}

// GetCameraProvisionLPRStatusHandler returns the Provision-ISR LPR heartbeat and status for a camera.
func (h *Handlers) GetCameraProvisionLPRStatusHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	idStr := chi.URLParam(r, "cameraId")
	camID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid camera id")
		return
	}

	cam, err := h.Inv.GetCamera(r.Context(), a, camID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "not_found", "camera not found")
		return
	}

	var cameraIP string
	if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
		if u, err := url.Parse(*cam.OnvifEndpoint); err == nil && u.Hostname() != "" {
			cameraIP = u.Hostname()
		}
	}
	streamHosts, _ := h.Inv.GetServerCameraStreamHosts(r.Context(), a, cam.ServerID)
	streamInfo := findStreamInfo(streamHosts, cam.RemoteName, cam.DisplayName)
	if cameraIP == "" && streamInfo.Host != "" {
		cameraIP = streamInfo.Host
	}

	res := map[string]any{
		"camera_id":       cam.ID,
		"camera_ip":       cameraIP,
		"online":          false,
		"keepalive_count": 0,
		"event_count":     0,
		"target_port":     7419,
		"keepalive_path":  "/SendKeepalive",
		"event_path":      "/v1/provision-lpr/event",
		"recent_events":   []any{},
		"recent_plates":   []any{},
	}

	if h.Provision != nil {
		if statusMap, err := h.Provision.GetProvisionLPRStatus(r.Context(), a, cam.ServerID); err == nil {
			if cMap := findProvisionCameraStatus(statusMap, cam.ID, cameraIP, cam.RemoteName, cam.DisplayName); cMap != nil {
				res["online"] = cMap["online"]
				res["last_seen"] = cMap["last_seen"]
				res["keepalive_count"] = cMap["keepalive_count"]
				res["event_count"] = cMap["event_count"]
				res["last_plate"] = cMap["last_plate"]
				res["last_event_time"] = cMap["last_event_time"]
				if recent, ok := cMap["recent_events"].([]any); ok && len(recent) > 0 {
					res["recent_events"] = recent
				}
			}
		}
	}

	if h.Events != nil {
		if page, err := h.Events.ListPlates(r.Context(), a, events.PlateFilter{CameraIDs: []uuid.UUID{cam.ID}, Limit: 10}); err == nil {
			plateItems := make([]map[string]any, 0, len(page.Items))
			for _, item := range page.Items {
				var vt, vc string
				if item.Vehicle != nil {
					vt = item.Vehicle.Type
					vc = item.Vehicle.Color
				}
				plateItems = append(plateItems, map[string]any{
					"id":            item.ID,
					"event_id":      item.EventID,
					"plate":         item.Plate,
					"seen_at":       item.SeenAt,
					"score":         item.Score,
					"vehicle_type":  vt,
					"vehicle_color": vc,
				})
			}
			res["recent_plates"] = plateItems
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// findProvisionCameraStatus finds a camera's entry in the agent's status; see provision.FindLPRCameraStatus.
func findProvisionCameraStatus(status map[string]any, camID uuid.UUID, cameraIP, remoteName, displayName string) map[string]any {
	return provision.FindLPRCameraStatus(status, camID, cameraIP, remoteName, displayName)
}

// registrationResult says whether the camera was registered on the agent and, if not, why.
type registrationResult struct {
	Registered bool
	Error      string
}

func registrationOutcome(err error) registrationResult {
	var statusErr *provision.AgentStatusError
	switch {
	case err == nil:
		return registrationResult{Registered: true}
	case errors.Is(err, provision.ErrAgentRegistryUnsupported):
		return registrationResult{Error: "agent_outdated"}
	case errors.As(err, &statusErr):
		return registrationResult{Error: statusErr.Message}
	default:
		return registrationResult{Error: "agent_unreachable"}
	}
}

// finishProvisionConfigure completes the agent's guided steps with what OpenVMS knows. Success means
// the camera is reporting to the agent, which only the camera's own keepalives can show: until then
// the answer stays the manual steps, whatever the registration did.
func finishProvisionConfigure(res map[string]any, camID uuid.UUID, reg registrationResult, entry map[string]any) map[string]any {
	online, _ := entry["online"].(bool)
	verified := entry != nil && online
	res["camera_id"] = camID.String()
	res["registered"] = reg.Registered
	if reg.Error != "" {
		res["registration_error"] = reg.Error
	}
	res["verified"] = verified
	res["success"] = verified
	if verified {
		res["method"] = "verified"
		res["message"] = "La cámara ya reporta a este agente: la integración está verificada."
	}
	return res
}

// targetHostFromBaseURL is the host of a Frigate server's URL, where its agent runs.
func targetHostFromBaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ConfigureCameraProvisionLPRHandler prepares a Provision-ISR camera to report to its server's agent:
// it registers the camera's address on the agent, returns the values to enter in the camera's web
// interface (the manufacturer documents no way to set them remotely), and says whether the camera is
// already reporting. It never sends credentials anywhere.
func (h *Handlers) ConfigureCameraProvisionLPRHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	camID, err := uuid.Parse(chi.URLParam(r, "cameraId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid camera id")
		return
	}
	cam, err := h.Inv.GetCamera(r.Context(), a, camID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "not_found", "camera not found")
		return
	}
	if _, err := h.Inv.RequireServerManage(r.Context(), a, cam.ServerID); err != nil {
		writeError(w, r, http.StatusForbidden, "forbidden", "permission servers.manage required")
		return
	}

	var req configureProvisionLPRBody
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req)
	}
	// The camera's credentials are not needed and are not forwarded.
	req.Username, req.Password = "", ""

	if req.CameraIP == "" {
		if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
			if u, err := url.Parse(*cam.OnvifEndpoint); err == nil && u.Hostname() != "" {
				req.CameraIP = u.Hostname()
			}
		}
		if req.CameraIP == "" {
			streamHosts, _ := h.Inv.GetServerCameraStreamHosts(r.Context(), a, cam.ServerID)
			if info := findStreamInfo(streamHosts, cam.RemoteName, cam.DisplayName); info.Host != "" {
				req.CameraIP = info.Host
			}
		}
	}
	if req.CameraIP == "" {
		writeError(w, r, http.StatusBadRequest, "camera_address_unknown", "the camera has no ONVIF endpoint or stream host to take its address from; enter it")
		return
	}
	if req.TargetHost == "" {
		if srv, err := h.Inv.GetServer(r.Context(), a, cam.ServerID); err == nil {
			req.TargetHost = targetHostFromBaseURL(srv.BaseUrl)
		}
	}
	if req.TargetPort <= 0 {
		req.TargetPort = 7419
	}
	if req.IntervalSecond <= 0 {
		req.IntervalSecond = 20
	}
	if req.KeepalivePath == "" {
		req.KeepalivePath = "/SendKeepalive"
	}
	if req.EventPath == "" {
		req.EventPath = "/v1/provision-lpr/event"
	}

	_, regErr := h.Provision.RegisterProvisionLPRCamera(r.Context(), a, cam.ServerID, cam.ID, req.CameraIP)
	reg := registrationOutcome(regErr)

	result, err := h.Provision.ConfigureProvisionLPR(r.Context(), a, cam.ServerID, req)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "configure_failed", err.Error())
		return
	}
	var entry map[string]any
	if status, err := h.Provision.GetProvisionLPRStatus(r.Context(), a, cam.ServerID); err == nil {
		entry = findProvisionCameraStatus(status, cam.ID, req.CameraIP, cam.RemoteName, cam.DisplayName)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(finishProvisionConfigure(result, cam.ID, reg, entry))
}

// UnregisterCameraProvisionLPRHandler removes a camera from its server agent's registry.
func (h *Handlers) UnregisterCameraProvisionLPRHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	camID, err := uuid.Parse(chi.URLParam(r, "cameraId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid camera id")
		return
	}
	cam, err := h.Inv.GetCamera(r.Context(), a, camID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "not_found", "camera not found")
		return
	}
	if err := h.Provision.UnregisterProvisionLPRCamera(r.Context(), a, cam.ServerID, cam.ID); err != nil {
		writeProvisionRegistryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetServerProvisionLPRRegistryHandler returns a server agent's camera registry, including the
// addresses that sent requests without being registered.
func (h *Handlers) GetServerProvisionLPRRegistryHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid server id")
		return
	}
	reg, err := h.Provision.GetProvisionLPRRegistry(r.Context(), a, serverID)
	if err != nil {
		writeProvisionRegistryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reg)
}

// GetServerProvisionLPRIngestionHandler reports how far central got collecting the plate readings
// that the server's cameras reported to its agent: last pull, backlog, totals and the last error.
func (h *Handlers) GetServerProvisionLPRIngestionHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid server id")
		return
	}
	state, err := h.Provision.GetProvisionLPRIngestion(r.Context(), a, serverID)
	switch {
	case errors.Is(err, access.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "permission servers.manage required")
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "ingestion_failed", "the ingestion state could not be read")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// SetServerProvisionLPREnforceHandler turns on or off the refusal of cameras that are not registered
// on a server's agent.
func (h *Handlers) SetServerProvisionLPREnforceHandler(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "invalid server id")
		return
	}
	var body struct {
		Enforce *bool `json:"enforce"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Enforce == nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", `a JSON body with "enforce": true or false is required`)
		return
	}
	reg, err := h.Provision.SetProvisionLPREnforce(r.Context(), a, serverID, *body.Enforce)
	if err != nil {
		writeProvisionRegistryError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reg)
}

func writeProvisionRegistryError(w http.ResponseWriter, r *http.Request, err error) {
	var statusErr *provision.AgentStatusError
	switch {
	case errors.Is(err, provision.ErrAgentRegistryUnsupported):
		writeError(w, r, http.StatusConflict, "agent_outdated", err.Error())
	case errors.As(err, &statusErr) && (statusErr.Status == http.StatusBadRequest || statusErr.Status == http.StatusConflict):
		writeError(w, r, statusErr.Status, "registry_rejected", statusErr.Message)
	case errors.Is(err, provision.ErrAgentProbeUnavailable):
		writeError(w, r, http.StatusBadGateway, "agent_unreachable", "the agent could not be reached")
	case errors.Is(err, access.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "permission servers.manage required")
	default:
		writeError(w, r, http.StatusBadGateway, "registry_failed", "the agent registry could not be read or changed")
	}
}
