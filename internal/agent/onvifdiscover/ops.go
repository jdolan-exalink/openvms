package onvifdiscover

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

type OpsHandler struct {
	token    string
	networks []netip.Prefix
}

func NewOpsHandler(token string, cfg Config) (*OpsHandler, error) {
	if strings.TrimSpace(token) == "" || len(cfg.AllowedCIDRs) == 0 {
		return nil, errors.New("ONVIF ops requires an agent token and allowed CIDRs")
	}
	h := &OpsHandler{token: token, networks: make([]netip.Prefix, 0, len(cfg.AllowedCIDRs))}
	for _, raw := range cfg.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() {
			return nil, errors.New("invalid ONVIF allowed CIDR")
		}
		h.networks = append(h.networks, prefix.Masked())
	}
	return h, nil
}

func (h *OpsHandler) allowed(address netip.Addr) bool {
	for _, network := range h.networks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

type SyncTimeRequest struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *OpsHandler) HandleSyncTime(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SyncTimeRequest
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

	client := onvif.NewClient(nil, onvif.Config{
		Timeout:     probeOperationTimeout,
		Credentials: creds,
	})
	device := onvif.NewDeviceClient(client, ep)

	now := time.Now().UTC()
	if err := device.SetSystemDateAndTime(r.Context(), now); err != nil {
		http.Error(w, "failed to sync camera date and time", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"synced_at": now.Format(time.RFC3339),
	})
}

type PTZRequest struct {
	Endpoint     string  `json:"endpoint"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	ProfileToken string  `json:"profile_token"`
	Action       string  `json:"action"` // continuous_move, stop, goto_preset, get_presets
	Pan          float64 `json:"pan,omitempty"`
	Tilt         float64 `json:"tilt,omitempty"`
	Zoom         float64 `json:"zoom,omitempty"`
	PresetToken  string  `json:"preset_token,omitempty"`
}

func (h *OpsHandler) HandlePTZ(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PTZRequest
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

	client := onvif.NewClient(nil, onvif.Config{
		Timeout:     probeOperationTimeout,
		Credentials: creds,
	})
	ptz := onvif.NewPTZClient(client, ep)

	switch req.Action {
	case "continuous_move":
		if err := ptz.ContinuousMove(r.Context(), req.ProfileToken, req.Pan, req.Tilt, req.Zoom, 5); err != nil {
			http.Error(w, "ptz continuous move failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

	case "stop":
		if err := ptz.Stop(r.Context(), req.ProfileToken, true, true); err != nil {
			http.Error(w, "ptz stop failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

	case "goto_preset":
		if err := ptz.GotoPreset(r.Context(), req.ProfileToken, req.PresetToken); err != nil {
			http.Error(w, "ptz goto preset failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

	case "get_presets":
		presets, err := ptz.GetPresets(r.Context(), req.ProfileToken)
		if err != nil {
			http.Error(w, "ptz get presets failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "presets": presets})

	default:
		http.Error(w, "unknown ptz action", http.StatusBadRequest)
	}
}

type VideoConfigRequest struct {
	Endpoint      string                           `json:"endpoint"`
	Username      string                           `json:"username"`
	Password      string                           `json:"password"`
	Action        string                           `json:"action"` // get, set
	Configuration *onvif.VideoEncoderConfiguration `json:"configuration,omitempty"`
}

func (h *OpsHandler) HandleVideoConfig(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req VideoConfigRequest
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

	client := onvif.NewClient(nil, onvif.Config{
		Timeout:     probeOperationTimeout,
		Credentials: creds,
	})
	media := onvif.NewMediaClient(client, ep)

	switch req.Action {
	case "get":
		cfgs, err := media.GetVideoEncoderConfigurations(r.Context())
		if err != nil {
			http.Error(w, "failed to get video encoder configurations", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "configurations": cfgs})

	case "set":
		if req.Configuration == nil {
			http.Error(w, "configuration required for set action", http.StatusBadRequest)
			return
		}
		if err := media.SetVideoEncoderConfiguration(r.Context(), *req.Configuration); err != nil {
			http.Error(w, "failed to set video encoder configuration", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

	default:
		http.Error(w, "unknown video config action", http.StatusBadRequest)
	}
}

type ImagingRequest struct {
	Endpoint         string                       `json:"endpoint"`
	Username         string                       `json:"username"`
	Password         string                       `json:"password"`
	VideoSourceToken string                       `json:"video_source_token"`
	Action           string                       `json:"action"` // get, options, set
	Settings         *onvif.ImagingSettingsUpdate `json:"settings,omitempty"`
}

func (h *OpsHandler) HandleImaging(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ImagingRequest
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

	client := onvif.NewClient(nil, onvif.Config{
		Timeout:     probeOperationTimeout,
		Credentials: creds,
	})
	imaging := onvif.NewImagingClient(client, ep)

	switch req.Action {
	case "get":
		s, err := imaging.GetImagingSettings(r.Context(), req.VideoSourceToken)
		if err != nil {
			http.Error(w, "failed to get imaging settings", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "settings": s})

	case "options":
		options, err := imaging.GetImagingOptions(r.Context(), req.VideoSourceToken)
		if err != nil {
			http.Error(w, "failed to get imaging options", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "options": options})

	case "set":
		if req.Settings == nil {
			http.Error(w, "settings required for set action", http.StatusBadRequest)
			return
		}
		if err := imaging.SetImagingSettings(r.Context(), req.VideoSourceToken, *req.Settings); err != nil {
			http.Error(w, "failed to set imaging settings", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

	default:
		http.Error(w, "unknown imaging action", http.StatusBadRequest)
	}
}

func (h *OpsHandler) authenticate(r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(h.token)) == 1
}
