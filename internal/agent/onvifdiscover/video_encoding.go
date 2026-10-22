package onvifdiscover

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

const videoEncodingOverallTimeout = 25 * time.Second

// VideoEncodingRequest reads which video encodings a camera offers ("options", read only) or
// switches one of its encoder configurations to H.264 or H.265 ("set").
type VideoEncodingRequest struct {
	Endpoint    string `json:"endpoint"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Action      string `json:"action"` // options, set
	ConfigToken string `json:"config_token,omitempty"`
	Encoding    string `json:"encoding,omitempty"` // H264 or H265
}

// EncoderConfigInfo is one video encoder configuration with the encodings it can be switched to.
type EncoderConfigInfo struct {
	Token              string   `json:"token"`
	Name               string   `json:"name"`
	Encoding           string   `json:"encoding"`
	Profile            string   `json:"profile,omitempty"`
	Width              int      `json:"width"`
	Height             int      `json:"height"`
	FPS                float64  `json:"fps"`
	Bitrate            int      `json:"bitrate"`
	SupportedEncodings []string `json:"supported_encodings"`
}

type VideoEncodingResult struct {
	Success bool `json:"success"`
	// Supported is false when the camera offers no Media2 service, the only one that exposes H.265.
	Supported        bool                `json:"supported"`
	Code             string              `json:"code,omitempty"`
	Detail           string              `json:"detail,omitempty"`
	Configurations   []EncoderConfigInfo `json:"configurations,omitempty"`
	PreviousEncoding string              `json:"previous_encoding,omitempty"`
	Encoding         string              `json:"encoding,omitempty"`
}

// videoEncodingOps holds what the operation needs from outside: an ONVIF client (injectable so it
// can be tested against a simulated camera) and the networks a camera may send the agent to.
type videoEncodingOps struct {
	client  *onvif.Client
	allowed func(netip.Addr) bool
}

// run executes the request against the camera behind device and returns the result with the HTTP
// status the handler should answer.
func (v videoEncodingOps) run(ctx context.Context, device onvif.Endpoint, req VideoEncodingRequest) (VideoEncodingResult, int) {
	target := normalizeTargetEncoding(req.Encoding)
	switch req.Action {
	case "options":
	case "set":
		if strings.TrimSpace(req.ConfigToken) == "" {
			return VideoEncodingResult{Code: "config_token_required"}, http.StatusBadRequest
		}
		if target == "" {
			return VideoEncodingResult{Code: "encoding_must_be_h264_or_h265"}, http.StatusBadRequest
		}
	default:
		return VideoEncodingResult{Code: "unknown_action"}, http.StatusBadRequest
	}

	media, found, err := v.media2Endpoint(ctx, device)
	if err != nil {
		return cameraError(err), http.StatusBadGateway
	}
	if !found {
		// Reading the options of a camera without Media2 is a valid answer ("nothing to offer"),
		// but a change that was never attempted must not read as a success.
		if req.Action == "set" {
			return VideoEncodingResult{Code: "media2_unsupported"}, http.StatusUnprocessableEntity
		}
		return VideoEncodingResult{Success: true, Code: "media2_unsupported"}, http.StatusOK
	}
	media2 := onvif.NewMedia2Client(v.client, media)

	configs, err := media2.GetVideoEncoderConfigurations(ctx)
	if err != nil {
		return cameraError(err), http.StatusBadGateway
	}

	if req.Action == "options" {
		infos := make([]EncoderConfigInfo, 0, len(configs))
		for _, c := range configs {
			info := configInfo(c)
			// A configuration whose options cannot be read is listed without alternatives.
			if opts, optErr := media2.GetVideoEncoderConfigurationOptions(ctx, c.Token); optErr == nil {
				info.SupportedEncodings = offeredEncodings(opts)
			}
			infos = append(infos, info)
		}
		return VideoEncodingResult{Success: true, Supported: true, Configurations: infos}, http.StatusOK
	}

	var current *onvif.VideoEncoder2Configuration
	for i := range configs {
		if configs[i].Token == req.ConfigToken {
			current = &configs[i]
			break
		}
	}
	if current == nil {
		return VideoEncodingResult{Supported: true, Code: "configuration_not_found"}, http.StatusNotFound
	}
	opts, err := media2.GetVideoEncoderConfigurationOptions(ctx, current.Token)
	if err != nil {
		return cameraError(err), http.StatusBadGateway
	}
	planned, err := onvif.PlanEncodingChange(*current, opts, target)
	switch {
	case errors.Is(err, onvif.ErrEncodingUnsupported):
		return VideoEncodingResult{Supported: true, Code: "encoding_unsupported"}, http.StatusUnprocessableEntity
	case errors.Is(err, onvif.ErrEncodingUnchanged):
		return VideoEncodingResult{Supported: true, Code: "encoding_unchanged"}, http.StatusConflict
	case err != nil:
		return VideoEncodingResult{Code: "plan_failed"}, http.StatusInternalServerError
	}
	if err := media2.SetVideoEncoderConfiguration(ctx, planned); err != nil {
		return cameraError(err), http.StatusBadGateway
	}

	// Some cameras accept the call and keep the old setting: read it back before saying it worked.
	after, err := media2.GetVideoEncoderConfigurations(ctx)
	if err != nil {
		return cameraError(err), http.StatusBadGateway
	}
	for _, c := range after {
		if c.Token == current.Token && strings.EqualFold(c.Encoding, planned.Encoding) {
			return VideoEncodingResult{Success: true, Supported: true, PreviousEncoding: current.Encoding, Encoding: c.Encoding}, http.StatusOK
		}
	}
	return VideoEncodingResult{Supported: true, Code: "not_applied", PreviousEncoding: current.Encoding}, http.StatusBadGateway
}

// media2Endpoint finds the camera's own Media2 service. Its address comes from the camera, so it is
// only used when it is an IPv4 inside the networks the agent is allowed to talk to.
func (v videoEncodingOps) media2Endpoint(ctx context.Context, device onvif.Endpoint) (onvif.Endpoint, bool, error) {
	devices := onvif.NewDeviceClient(v.client, device)
	services, err := devices.GetServices(ctx)
	if err != nil {
		var callErr *onvif.CallError
		if !errors.As(err, &callErr) {
			return onvif.Endpoint{}, false, err
		}
		if services, err = devices.GetCapabilities(ctx); err != nil {
			return onvif.Endpoint{}, false, err
		}
	}
	for _, s := range services {
		if !strings.Contains(strings.ToLower(s.Namespace), "ver20/media") {
			continue
		}
		endpoint, err := s.ValidatedEndpoint()
		if err != nil {
			continue
		}
		u, err := url.Parse(endpoint.String())
		if err != nil {
			continue
		}
		address, err := netip.ParseAddr(u.Hostname())
		if err != nil || !address.Is4() || !v.allowed(address) {
			continue
		}
		return endpoint, true, nil
	}
	return onvif.Endpoint{}, false, nil
}

func cameraError(err error) VideoEncodingResult {
	res := VideoEncodingResult{Code: "camera_error"}
	var callErr *onvif.CallError
	if errors.As(err, &callErr) && callErr.Code != "" {
		res.Detail = callErr.Code
	}
	return res
}

func configInfo(c onvif.VideoEncoder2Configuration) EncoderConfigInfo {
	return EncoderConfigInfo{
		Token: c.Token, Name: c.Name, Encoding: c.Encoding, Profile: c.Profile,
		Width: c.Resolution.Width, Height: c.Resolution.Height,
		FPS: c.RateControl.FrameRateLimit, Bitrate: c.RateControl.BitrateLimit,
		SupportedEncodings: []string{},
	}
}

// offeredEncodings lists the encodings a camera declared, H.265 and H.264 spelled the ONVIF way.
func offeredEncodings(opts []onvif.VideoEncoder2Options) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		e := strings.ToUpper(strings.TrimSpace(o.Encoding))
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// normalizeTargetEncoding accepts only the two encodings this operation is meant for.
func normalizeTargetEncoding(encoding string) string {
	switch strings.ToUpper(strings.NewReplacer(".", "", "-", "", " ", "").Replace(strings.TrimSpace(encoding))) {
	case "H264", "AVC":
		return "H264"
	case "H265", "HEVC":
		return "H265"
	}
	return ""
}

// HandleVideoEncoding is POST /v1/onvif/video-encoding.
func (h *OpsHandler) HandleVideoEncoding(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req VideoEncodingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProbeRequestBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	device, address, err := parseProbeEndpoint(req.Endpoint)
	if err != nil || !h.allowed(address) {
		http.Error(w, "invalid or disallowed endpoint", http.StatusBadRequest)
		return
	}
	var creds *onvif.Credentials
	if req.Username != "" {
		creds = onvif.NewCredentials(req.Username, req.Password)
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ops := videoEncodingOps{
		client:  onvif.NewClient(transport, onvif.Config{Timeout: probeOperationTimeout, MaxBodyBytes: maxProbeResponseBytes, Credentials: creds}),
		allowed: h.allowed,
	}
	ctx, cancel := context.WithTimeout(r.Context(), videoEncodingOverallTimeout)
	defer cancel()
	res, status := ops.run(ctx, device, req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}
