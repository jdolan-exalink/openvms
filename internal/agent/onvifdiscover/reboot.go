package onvifdiscover

import (
	"encoding/json"
	"net/http"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

// RebootRequest asks the agent to restart a camera over ONVIF SystemReboot.
type RebootRequest struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleReboot is POST /v1/onvif/reboot (scope control). It answers {"success":true} once the
// camera accepted the command; the camera is offline for a while after that.
func (h *OpsHandler) HandleReboot(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req RebootRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProbeRequestBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ep, address, err := parseProbeEndpoint(req.Endpoint)
	if err != nil || !h.allowed(address) {
		http.Error(w, "invalid or disallowed endpoint", http.StatusBadRequest)
		return
	}
	var creds *onvif.Credentials
	if req.Username != "" {
		creds = onvif.NewCredentials(req.Username, req.Password)
	}
	client := onvif.NewClient(h.onvifTransport, onvif.Config{Timeout: probeOperationTimeout, Credentials: creds})
	if err := onvif.NewDeviceClient(client, ep).SystemReboot(r.Context()); err != nil {
		http.Error(w, "camera reboot failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}
