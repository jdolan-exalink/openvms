package api

import (
	"net/http"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestVideoEncodingResponseMapsTheAgentsAnswer(t *testing.T) {
	t.Run("a successful set is a 200 with both encodings", func(t *testing.T) {
		got := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Success: true, Supported: true, PreviousEncoding: "H265", Encoding: "H264"}, http.StatusOK)
		ok, isOK := got.(gen.OperateCameraOnvifVideoEncoding200JSONResponse)
		if !isOK || !ok.Success || !ok.Supported || ok.PreviousEncoding == nil || *ok.PreviousEncoding != "H265" || ok.Encoding == nil || *ok.Encoding != "H264" {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("the camera options keep every field", func(t *testing.T) {
		res := onvifdiscover.VideoEncodingResult{Success: true, Supported: true, Configurations: []onvifdiscover.EncoderConfigInfo{{
			Token: "enc_main", Name: "Main", Encoding: "H265", Profile: "Main", Width: 2560, Height: 1440, FPS: 20, Bitrate: 4096, SupportedEncodings: []string{"H265", "H264"},
		}}}
		ok := videoEncodingResponse(res, http.StatusOK).(gen.OperateCameraOnvifVideoEncoding200JSONResponse)
		if ok.Configurations == nil || len(*ok.Configurations) != 1 {
			t.Fatalf("configurations: %#v", ok.Configurations)
		}
		c := (*ok.Configurations)[0]
		if c.Token != "enc_main" || c.Encoding != "H265" || c.Width != 2560 || c.Fps != 20 || c.Bitrate != 4096 || len(c.SupportedEncodings) != 2 || c.Profile == nil || *c.Profile != "Main" {
			t.Fatalf("configuration: %#v", c)
		}
	})

	t.Run("a camera without Media2 is a 200 that says unsupported", func(t *testing.T) {
		ok := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Success: true, Code: "media2_unsupported"}, http.StatusOK).(gen.OperateCameraOnvifVideoEncoding200JSONResponse)
		if ok.Supported || ok.Code == nil || *ok.Code != "media2_unsupported" {
			t.Fatalf("got %#v", ok)
		}
	})

	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusUnprocessableEntity, "encoding_unsupported"},
		{http.StatusConflict, "encoding_unchanged"},
		{http.StatusNotFound, "configuration_not_found"},
	} {
		t.Run("a refused change is a 200 with success false and the reason: "+tc.code, func(t *testing.T) {
			ok, isOK := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Supported: true, Code: tc.code}, tc.status).(gen.OperateCameraOnvifVideoEncoding200JSONResponse)
			if !isOK || ok.Success || ok.Code == nil || *ok.Code != tc.code {
				t.Fatalf("got %#v", ok)
			}
		})
	}

	t.Run("a bad request stays a 400", func(t *testing.T) {
		bad, isBad := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Code: "encoding_must_be_h264_or_h265"}, http.StatusBadRequest).(gen.OperateCameraOnvifVideoEncoding400JSONResponse)
		if !isBad || bad.Code != "encoding_must_be_h264_or_h265" {
			t.Fatalf("got %#v", bad)
		}
	})

	t.Run("a change the camera did not apply is a 502 carrying the code", func(t *testing.T) {
		bad, isBad := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Supported: true, Code: "not_applied"}, http.StatusBadGateway).(gen.OperateCameraOnvifVideoEncoding502JSONResponse)
		if !isBad || bad.Code != "not_applied" || bad.Message == "" {
			t.Fatalf("got %#v", bad)
		}
	})

	t.Run("camera errors expose the ONVIF reason without credentials", func(t *testing.T) {
		bad := videoEncodingResponse(onvifdiscover.VideoEncodingResult{Code: "camera_error", Detail: "authentication_failed"}, http.StatusBadGateway).(gen.OperateCameraOnvifVideoEncoding502JSONResponse)
		if bad.Code != "camera_error" || bad.Message == "" {
			t.Fatalf("got %#v", bad)
		}
	})
}
