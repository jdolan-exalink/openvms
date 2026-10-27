package camconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/onvif"
)

func agentDevice(call AgentVideoCall) *AgentVideoDevice {
	return &AgentVideoDevice{Endpoint: "http://192.168.1.50/onvif/device_service", Username: "admin", Password: "hunter2-secret", Call: call}
}

func answer(res onvifdiscover.VideoDeviceResult) AgentVideoCall {
	return func(context.Context, onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
		return res, 200, nil
	}
}

func TestAgentVideoDeviceReadsAndSendsTheRightOperations(t *testing.T) {
	var seen []onvifdiscover.VideoDeviceRequest
	d := agentDevice(func(_ context.Context, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
		seen = append(seen, req)
		switch req.Op {
		case onvifdiscover.VideoDeviceOpStreams:
			return onvifdiscover.VideoDeviceResult{Success: true, Streams: []onvif.ProfileStream{{Token: "p1", Codec: "H264"}}}, 200, nil
		case onvifdiscover.VideoDeviceOpOptions:
			return onvifdiscover.VideoDeviceResult{Success: true, Options: &onvif.VideoOptions{Codecs: []onvif.CodecOptions{{Codec: "H264"}}}}, 200, nil
		}
		return onvifdiscover.VideoDeviceResult{Success: true, Stream: &onvif.ProfileStream{Token: req.ProfileToken, FPS: *req.Desired.FPS}}, 200, nil
	})
	ctx := context.Background()
	streams, err := d.Streams(ctx)
	if err != nil || len(streams) != 1 || streams[0].Token != "p1" {
		t.Fatalf("streams %+v %v", streams, err)
	}
	opts, err := d.Options(ctx, "p1")
	if err != nil || len(opts.Codecs) != 1 {
		t.Fatalf("options %+v %v", opts, err)
	}
	fps := 10.0
	got, err := d.Set(ctx, "p1", onvif.VideoDesired{FPS: &fps})
	if err != nil || got.FPS != 10 {
		t.Fatalf("set %+v %v", got, err)
	}
	if len(seen) != 3 || seen[1].ProfileToken != "p1" || seen[2].Op != "set_video_encoder" || seen[0].Username != "admin" || seen[0].Endpoint == "" {
		t.Fatalf("requests: %+v", seen)
	}
}

func TestAgentVideoDeviceRestoresTheTypedErrors(t *testing.T) {
	ctx := context.Background()
	fps := 99.0
	var ve *onvif.ValidationError
	_, err := agentDevice(answer(onvifdiscover.VideoDeviceResult{Code: "validation_failed", Violations: []onvif.Violation{{Field: "fps", Message: "fuera de rango"}}})).Set(ctx, "p", onvif.VideoDesired{FPS: &fps})
	if !errors.As(err, &ve) || len(ve.Violations) != 1 {
		t.Fatalf("validation: %v", err)
	}
	var verr *onvif.VerifyError
	stream, err := agentDevice(answer(onvifdiscover.VideoDeviceResult{Code: "not_applied", Fields: []string{"fps"}, Stream: &onvif.ProfileStream{FPS: 20}})).Set(ctx, "p", onvif.VideoDesired{FPS: &fps})
	if !errors.As(err, &verr) || stream.FPS != 20 {
		t.Fatalf("not applied: %v %+v", err, stream)
	}
	for code, want := range map[string]error{"profile_not_found": onvif.ErrProfileNotFound, "no_video_encoder": onvif.ErrNoVideoEncoder, "gop_not_writable": onvif.ErrGOPNotWritable, "unknown_op": ErrAgentOutdated} {
		if _, err := agentDevice(answer(onvifdiscover.VideoDeviceResult{Code: code})).Options(ctx, "p"); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", code, err, want)
		}
	}
}

func TestAgentVideoDeviceFailuresReadAsReasonsAndNeverCarryCredentials(t *testing.T) {
	ctx := context.Background()
	outdated := agentDevice(func(context.Context, onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
		return onvifdiscover.VideoDeviceResult{}, 0, ErrAgentOutdated
	})
	_, err := outdated.Streams(ctx)
	if reasonFor(err) != VideoAgentOutdated || VideoReasonText(VideoAgentOutdated) != "Actualizá el agente para configurar el video de esta cámara." {
		t.Fatalf("outdated: %v / %q", err, VideoReasonText(VideoAgentOutdated))
	}
	camera := agentDevice(answer(onvifdiscover.VideoDeviceResult{Code: "camera_error", Detail: "soap_fault"}))
	_, err = camera.Streams(ctx)
	if reasonFor(err) != VideoUnreachable {
		t.Fatalf("camera error reason = %s", reasonFor(err))
	}
	down := agentDevice(func(context.Context, onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
		return onvifdiscover.VideoDeviceResult{}, 0, errors.New("registered agent probe unavailable")
	})
	_, err = down.Streams(ctx)
	if reasonFor(err) != VideoUnreachable {
		t.Fatalf("agent down reason = %s", reasonFor(err))
	}
	for _, e := range []error{err, agentVideoError(onvifdiscover.VideoDeviceResult{Code: "camera_error"})} {
		for _, secret := range []string{"hunter2-secret", "admin", "192.168.1.50"} {
			if strings.Contains(e.Error(), secret) {
				t.Errorf("error leaks %q: %v", secret, e)
			}
		}
	}
	if _, err := (&AgentVideoDevice{}).Streams(ctx); reasonFor(err) != VideoUnreachable {
		t.Errorf("no caller: %v", err)
	}
}

func TestOutdatedAgentSurfacesThroughTheVideoOptionsReport(t *testing.T) {
	d := agentDevice(func(context.Context, onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
		return onvifdiscover.VideoDeviceResult{}, 0, ErrAgentOutdated
	})
	rep := BuildVideoOptions(context.Background(), "cam", &VideoLink{Device: d, PrimaryToken: "p1"})
	if rep.Available || rep.Reason != VideoAgentOutdated {
		t.Fatalf("report: %+v", rep)
	}
	if videoApplyError("principal", ErrAgentOutdated).Error() != "stream principal: Actualizá el agente para configurar el video de esta cámara." {
		t.Fatalf("apply error: %v", videoApplyError("principal", ErrAgentOutdated))
	}
}
