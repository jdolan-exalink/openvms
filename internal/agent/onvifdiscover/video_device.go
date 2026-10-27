package onvifdiscover

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

const videoDeviceOverallTimeout = 30 * time.Second

// Operations of POST /v1/onvif/video-device. They let OpenVMS read and change the video of a camera
// that only the agent can reach (a LAN behind the site), with the same building blocks the API uses
// when it reaches the camera directly.
const (
	VideoDeviceOpStreams = "profile_streams"
	VideoDeviceOpOptions = "video_options"
	VideoDeviceOpSet     = "set_video_encoder"
)

// Result codes of the video-device operation (VideoDeviceResult.Code).
const (
	VideoDeviceCameraError     = "camera_error"
	VideoDeviceProfileNotFound = "profile_not_found"
	VideoDeviceNoEncoder       = "no_video_encoder"
	VideoDeviceGOPNotWritable  = "gop_not_writable"
	VideoDeviceNotSupported    = "validation_failed"
	VideoDeviceNotApplied      = "not_applied"
	VideoDeviceUnknownOp       = "unknown_op"
	VideoDeviceBadRequest      = "bad_request"
)

// VideoDeviceRequest is one video operation against a camera. The credentials travel with the
// request exactly as in the other ONVIF operations and are never logged or returned.
type VideoDeviceRequest struct {
	Endpoint     string              `json:"endpoint"`
	Username     string              `json:"username"`
	Password     string              `json:"password"`
	Op           string              `json:"op"`
	ProfileToken string              `json:"profile_token,omitempty"`
	Desired      *onvif.VideoDesired `json:"desired,omitempty"`
}

// VideoDeviceResult carries what the camera answered, or why it could not. Violations and Fields
// hold the structured reasons of a refused or unapplied change; nothing here quotes the camera's
// own error text or any credential.
type VideoDeviceResult struct {
	Success    bool                  `json:"success"`
	Code       string                `json:"code,omitempty"`
	Detail     string                `json:"detail,omitempty"`
	Streams    []onvif.ProfileStream `json:"streams,omitempty"`
	Options    *onvif.VideoOptions   `json:"options,omitempty"`
	Stream     *onvif.ProfileStream  `json:"stream,omitempty"`
	Violations []onvif.Violation     `json:"violations,omitempty"`
	Fields     []string              `json:"fields,omitempty"`
}

// videoDeviceOps runs a request against a camera whose ONVIF client is injected (tests use a
// simulated camera).
type videoDeviceOps struct {
	client *onvif.Client
}

func (v videoDeviceOps) run(ctx context.Context, endpoint string, req VideoDeviceRequest) (VideoDeviceResult, int) {
	switch req.Op {
	case VideoDeviceOpStreams:
	case VideoDeviceOpOptions, VideoDeviceOpSet:
		if strings.TrimSpace(req.ProfileToken) == "" || len(req.ProfileToken) > maxProbeFieldRunes {
			return VideoDeviceResult{Code: VideoDeviceBadRequest, Detail: "profile_token_required"}, http.StatusBadRequest
		}
		if req.Op == VideoDeviceOpSet && req.Desired == nil {
			return VideoDeviceResult{Code: VideoDeviceBadRequest, Detail: "desired_required"}, http.StatusBadRequest
		}
	default:
		return VideoDeviceResult{Code: VideoDeviceUnknownOp}, http.StatusBadRequest
	}
	conn, err := onvif.DiscoverConn(ctx, v.client, endpoint)
	if err != nil {
		return cameraFailure(err), http.StatusBadGateway
	}
	dev := onvif.ConnDevice{Conn: conn}
	switch req.Op {
	case VideoDeviceOpStreams:
		streams, err := dev.Streams(ctx)
		if err != nil {
			return cameraFailure(err), http.StatusBadGateway
		}
		return VideoDeviceResult{Success: true, Streams: streams}, http.StatusOK
	case VideoDeviceOpOptions:
		opts, err := dev.Options(ctx, req.ProfileToken)
		if err != nil {
			return failureOf(err)
		}
		return VideoDeviceResult{Success: true, Options: &opts}, http.StatusOK
	default:
		stream, err := dev.Set(ctx, req.ProfileToken, *req.Desired)
		if err != nil {
			res, status := failureOf(err)
			if res.Code == VideoDeviceNotApplied {
				res.Stream = &stream
			}
			return res, status
		}
		return VideoDeviceResult{Success: true, Stream: &stream}, http.StatusOK
	}
}

// failureOf maps the errors of the video building blocks to a code the central side turns back into
// the same typed error. The error text is dropped: it could quote the camera.
func failureOf(err error) (VideoDeviceResult, int) {
	var ve *onvif.ValidationError
	var verr *onvif.VerifyError
	switch {
	case errors.As(err, &ve):
		return VideoDeviceResult{Code: VideoDeviceNotSupported, Violations: ve.Violations}, http.StatusUnprocessableEntity
	case errors.As(err, &verr):
		return VideoDeviceResult{Code: VideoDeviceNotApplied, Fields: verr.Fields}, http.StatusConflict
	case errors.Is(err, onvif.ErrProfileNotFound):
		return VideoDeviceResult{Code: VideoDeviceProfileNotFound}, http.StatusUnprocessableEntity
	case errors.Is(err, onvif.ErrNoVideoEncoder):
		return VideoDeviceResult{Code: VideoDeviceNoEncoder}, http.StatusUnprocessableEntity
	case errors.Is(err, onvif.ErrGOPNotWritable):
		return VideoDeviceResult{Code: VideoDeviceGOPNotWritable}, http.StatusUnprocessableEntity
	}
	return cameraFailure(err), http.StatusBadGateway
}

func cameraFailure(err error) VideoDeviceResult {
	res := VideoDeviceResult{Code: VideoDeviceCameraError}
	var callErr *onvif.CallError
	if errors.As(err, &callErr) && callErr.Code != "" {
		res.Detail = callErr.Code
	}
	return res
}

// HandleVideoDevice is POST /v1/onvif/video-device (scope control).
func (h *OpsHandler) HandleVideoDevice(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req VideoDeviceRequest
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
	transport := h.onvifTransport
	if transport == nil {
		transport = &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	ops := videoDeviceOps{client: onvif.NewClient(transport, onvif.Config{Timeout: probeOperationTimeout, MaxBodyBytes: maxProbeResponseBytes, Credentials: creds})}
	ctx, cancel := context.WithTimeout(r.Context(), videoDeviceOverallTimeout)
	defer cancel()
	res, status := ops.run(ctx, device.String(), req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}
