package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func (h *Handlers) OperateCameraOnvifVideoEncoding(ctx context.Context, r gen.OperateCameraOnvifVideoEncodingRequestObject) (gen.OperateCameraOnvifVideoEncodingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return gen.OperateCameraOnvifVideoEncoding400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_request", Message: "Request body is required"},
		}, nil
	}
	if h.Provision == nil {
		return gen.OperateCameraOnvifVideoEncoding400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "provision_unavailable", Message: "Provisioning service unavailable"},
		}, nil
	}
	endpoint, username, password := h.resolveCameraOnvifCredentials(ctx, a, cam, r.Body.Endpoint, r.Body.Username, r.Body.Password)
	if endpoint == "" {
		return gen.OperateCameraOnvifVideoEncoding400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"},
		}, nil
	}

	req := onvifdiscover.VideoEncodingRequest{Endpoint: endpoint, Username: username, Password: password, Action: string(r.Body.Action)}
	if r.Body.ConfigToken != nil {
		req.ConfigToken = *r.Body.ConfigToken
	}
	if r.Body.Encoding != nil {
		req.Encoding = string(*r.Body.Encoding)
	}

	res, status, err := h.Provision.ExecuteOnvifVideoEncoding(ctx, a, cam.ServerID, req)
	if err != nil {
		if errors.Is(err, access.ErrForbidden) || errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return gen.OperateCameraOnvifVideoEncoding502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "video_encoding_failed", Message: "The server's agent could not be reached or did not answer the request"},
		}, nil
	}
	if req.Action == "set" && status == http.StatusOK && res.Success {
		if auditErr := h.Inv.AuditCameraVideoEncoding(ctx, a, cam.TenantID, cam.ID, req.ConfigToken, res.PreviousEncoding, res.Encoding); auditErr != nil {
			// The camera already changed: report that, and make the missing record loud.
			h.Log.ErrorContext(ctx, "audit camera video encoding change", "camera_id", cam.ID, "error", auditErr)
		}
	}
	return videoEncodingResponse(res, status), nil
}

// videoEncodingConfig is the generated anonymous element type of the result's configurations.
type videoEncodingConfig = struct {
	Bitrate            int      `json:"bitrate"`
	Encoding           string   `json:"encoding"`
	Fps                float32  `json:"fps"`
	Height             int      `json:"height"`
	Name               string   `json:"name"`
	Profile            *string  `json:"profile,omitempty"`
	SupportedEncodings []string `json:"supported_encodings"`
	Token              string   `json:"token"`
	Width              int      `json:"width"`
}

// videoEncodingResponse maps what the agent answered to the API. A change the camera refused or
// could not honour (unsupported or unchanged encoding, unknown configuration) is a 200 whose body
// says why, so a client reads one shape; a malformed request stays a 400 and a camera that failed
// or ignored the change is a 502.
func videoEncodingResponse(res onvifdiscover.VideoEncodingResult, status int) gen.OperateCameraOnvifVideoEncodingResponseObject {
	switch status {
	case http.StatusBadRequest:
		return gen.OperateCameraOnvifVideoEncoding400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: res.Code, Message: videoEncodingMessage(res)},
		}
	case http.StatusBadGateway:
		return gen.OperateCameraOnvifVideoEncoding502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: res.Code, Message: videoEncodingMessage(res)},
		}
	}
	out := gen.OperateCameraOnvifVideoEncoding200JSONResponse{Success: res.Success && status == http.StatusOK, Supported: res.Supported}
	if res.Code != "" {
		out.Code = &res.Code
	}
	if res.Detail != "" {
		out.Detail = &res.Detail
	}
	if res.PreviousEncoding != "" {
		out.PreviousEncoding = &res.PreviousEncoding
	}
	if res.Encoding != "" {
		out.Encoding = &res.Encoding
	}
	if len(res.Configurations) > 0 {
		cfgs := make([]videoEncodingConfig, 0, len(res.Configurations))
		for _, c := range res.Configurations {
			var item videoEncodingConfig
			item.Bitrate, item.Encoding, item.Fps, item.Height = c.Bitrate, c.Encoding, float32(c.FPS), c.Height
			item.Name, item.SupportedEncodings, item.Token, item.Width = c.Name, c.SupportedEncodings, c.Token, c.Width
			if c.Profile != "" {
				profile := c.Profile
				item.Profile = &profile
			}
			cfgs = append(cfgs, item)
		}
		out.Configurations = &cfgs
	}
	return out
}

func videoEncodingMessage(res onvifdiscover.VideoEncodingResult) string {
	// #nosec G101 -- these are error codes and messages, not credentials.
	msg := map[string]string{
		"not_applied":                   "The camera accepted the change but kept its previous video encoding",
		"camera_error":                  "The camera did not answer the ONVIF request",
		"unknown_action":                "Unknown action",
		"config_token_required":         "config_token is required to change an encoding",
		"encoding_must_be_h264_or_h265": "encoding must be H264 or H265",
	}[res.Code]
	if msg == "" {
		msg = "The video encoding operation failed"
	}
	if res.Detail != "" {
		msg += " (" + strings.ReplaceAll(res.Detail, "_", " ") + ")"
	}
	return msg
}
