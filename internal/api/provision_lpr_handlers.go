package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/events"
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
			if camerasList, ok := statusMap["cameras"].([]any); ok {
				for _, item := range camerasList {
					if cMap, ok := item.(map[string]any); ok {
						cIP, _ := cMap["camera_ip"].(string)
						cID, _ := cMap["camera_id"].(string)
						if (cameraIP != "" && cIP == cameraIP) || (cID != "" && (cID == cam.RemoteName || cID == cam.DisplayName || cID == cam.ID.String())) {
							res["online"] = cMap["online"]
							res["last_seen"] = cMap["last_seen"]
							res["keepalive_count"] = cMap["keepalive_count"]
							res["event_count"] = cMap["event_count"]
							res["last_plate"] = cMap["last_plate"]
							res["last_event_time"] = cMap["last_event_time"]
							if recent, ok := cMap["recent_events"].([]any); ok && len(recent) > 0 {
								res["recent_events"] = recent
							}
							break
						}
					}
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

// ConfigureCameraProvisionLPRHandler applies the HTTP POST LPR configuration on a Provision-ISR camera.
func (h *Handlers) ConfigureCameraProvisionLPRHandler(w http.ResponseWriter, r *http.Request) {
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

	if _, err := h.Inv.RequireServerManage(r.Context(), a, cam.ServerID); err != nil {
		writeError(w, r, http.StatusForbidden, "forbidden", "permission servers.manage required")
		return
	}

	var req configureProvisionLPRBody
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.CameraIP == "" {
		if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
			if u, err := url.Parse(*cam.OnvifEndpoint); err == nil && u.Hostname() != "" {
				req.CameraIP = u.Hostname()
			}
		}
		if req.CameraIP == "" {
			streamHosts, _ := h.Inv.GetServerCameraStreamHosts(r.Context(), a, cam.ServerID)
			info := findStreamInfo(streamHosts, cam.RemoteName, cam.DisplayName)
			if info.Host != "" {
				req.CameraIP = info.Host
			}
		}
	}

	if req.Username == "" {
		streamHosts, _ := h.Inv.GetServerCameraStreamHosts(r.Context(), a, cam.ServerID)
		info := findStreamInfo(streamHosts, cam.RemoteName, cam.DisplayName)
		if info.Username != "" {
			req.Username = info.Username
			req.Password = info.Password
		}
		if req.Username == "" {
			if srvUser, srvPass, err := h.Inv.GetServerCredentials(r.Context(), a, cam.ServerID); err == nil && srvUser != "" {
				req.Username = srvUser
				req.Password = srvPass
			}
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

	result, err := h.Provision.ConfigureProvisionLPR(r.Context(), a, cam.ServerID, req)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "configure_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
