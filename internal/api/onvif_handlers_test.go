package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestCheckOnvifRoutesAreRegisteredAndRequireActor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("check_camera_onvif", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/check", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("check_server_cameras_onvif", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/onvif/check-cameras", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("add_camera_to_frigate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/frigate/add-camera", strings.NewReader(`{"name":"cam1","main_stream_url":"rtsp://10.0.0.1/live"}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("sync_camera_onvif_time", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/sync-time", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("get_camera_onvif_snapshot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/snapshot", strings.NewReader(`{"profile_token":"main"}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("control_camera_onvif_ptz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/ptz", strings.NewReader(`{"action":"stop"}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("operate_camera_onvif_video_config", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/video-config", strings.NewReader(`{"action":"get"}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("operate_camera_onvif_imaging", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/onvif/imaging", strings.NewReader(`{"action":"get"}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("camera_provision_lpr_status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/provision-lpr/status", nil)
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})

	t.Run("camera_provision_lpr_configure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/11111111-1111-1111-1111-111111111111/provision-lpr/configure", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, req)
		if rw.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d body=%s; want unauthorized", rw.Code, rw.Body.String())
		}
	})
}

func TestParseOnvifProbeCapabilities(t *testing.T) {
	t.Run("profile_t_with_analytics_and_ptz", func(t *testing.T) {
		res := provision.OnvifProbeResult{
			Information: provision.OnvifProbeDeviceInformation{
				Manufacturer:    "Dahua",
				Model:           "IPC-HFW4431R-Z",
				FirmwareVersion: "V2.800.0000000.10.R",
				SerialNumber:    "DH12345678",
				HardwareID:      "1.00",
			},
			Services: []provision.OnvifProbeService{
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddrs: []string{"http://10.1.1.227/media2"}, Version: provision.OnvifProbeVersion{Major: 2, Minor: 40}},
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddrs: []string{"http://10.1.1.227/media1"}, Version: provision.OnvifProbeVersion{Major: 1, Minor: 20}},
				{Namespace: "http://www.onvif.org/ver20/analytics/wsdl", XAddrs: []string{"http://10.1.1.227/analytics"}, Version: provision.OnvifProbeVersion{Major: 2, Minor: 20}},
				{Namespace: "http://www.onvif.org/ver10/events/wsdl", XAddrs: []string{"http://10.1.1.227/events"}, Version: provision.OnvifProbeVersion{Major: 1, Minor: 10}},
				{Namespace: "http://www.onvif.org/ver20/ptz/wsdl", XAddrs: []string{"http://10.1.1.227/ptz"}, Version: provision.OnvifProbeVersion{Major: 2, Minor: 10}},
			},
		}

		profile, version, caps := parseOnvifProbeCapabilities(res)
		if profile != "ONVIF-T" {
			t.Errorf("profile = %q, want ONVIF-T", profile)
		}
		if version != "2.0" {
			t.Errorf("version = %q, want 2.0", version)
		}
		if caps.Manufacturer == nil || *caps.Manufacturer != "Dahua" {
			t.Errorf("manufacturer = %v, want Dahua", caps.Manufacturer)
		}
		if caps.HasAnalytics == nil || !*caps.HasAnalytics {
			t.Errorf("has_analytics = %v, want true", caps.HasAnalytics)
		}
		if caps.HasEvents == nil || !*caps.HasEvents {
			t.Errorf("has_events = %v, want true", caps.HasEvents)
		}
		if caps.HasPtz == nil || !*caps.HasPtz {
			t.Errorf("has_ptz = %v, want true", caps.HasPtz)
		}
		if caps.HasMedia2 == nil || !*caps.HasMedia2 {
			t.Errorf("has_media2 = %v, want true", caps.HasMedia2)
		}
		if caps.Profiles == nil || len(*caps.Profiles) < 2 {
			t.Errorf("profiles = %v, want at least 2 profiles", caps.Profiles)
		}
	})

	t.Run("profile_s_legacy", func(t *testing.T) {
		res := provision.OnvifProbeResult{
			Information: provision.OnvifProbeDeviceInformation{
				Manufacturer: "Generic",
				Model:        "CAM1",
			},
			Services: []provision.OnvifProbeService{
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddrs: []string{"http://10.1.1.220/media1"}, Version: provision.OnvifProbeVersion{Major: 1, Minor: 10}},
			},
		}

		profile, version, caps := parseOnvifProbeCapabilities(res)
		if profile != "ONVIF-S" {
			t.Errorf("profile = %q, want ONVIF-S", profile)
		}
		if version != "1.0" {
			t.Errorf("version = %q, want 1.0", version)
		}
		if caps.HasMedia2 == nil || *caps.HasMedia2 {
			t.Errorf("has_media2 = %v, want false", caps.HasMedia2)
		}
		if caps.HasMedia1 == nil || !*caps.HasMedia1 {
			t.Errorf("has_media1 = %v, want true", caps.HasMedia1)
		}
	})
}

func TestParseOnvifProbeCapabilitiesDetectsTheAnalyticsTheCameraAdvertises(t *testing.T) {
	res := provision.OnvifProbeResult{
		Information: provision.OnvifProbeDeviceInformation{Manufacturer: "Provision-ISR", Model: "I6-340"},
		Services: []provision.OnvifProbeService{
			{Namespace: "http://www.onvif.org/ver10/events/wsdl", XAddrs: []string{"http://10.0.15.107/events"}, Version: provision.OnvifProbeVersion{Major: 1, Minor: 10}},
		},
	}
	res.Analytics.SupportedTopics = []string{"RuleEngine/LineDetector/Crossed", "VideoSource/MotionAlarm", "Device/Offline/State"}

	_, _, caps := parseOnvifProbeCapabilities(res)
	got, ok := caps.Get("analytics")
	if !ok {
		t.Fatal("analytics capability set is missing")
	}
	detected, ok := got.([]onvif.AnalyticsCapability)
	if !ok || len(detected) != 2 || detected[0].ID != "line_crossing" || detected[1].ID != "motion" {
		t.Fatalf("analytics = %#v, want line_crossing and motion only", got)
	}
	if detected[0].Status != onvif.AnalyticsAdvertised || len(detected[0].Topics) != 1 {
		t.Fatalf("capability %+v must say it is only advertised and keep its evidence", detected[0])
	}

	res.Analytics.SupportedTopics = nil
	if _, _, caps := parseOnvifProbeCapabilities(res); caps.AdditionalProperties != nil {
		if _, present := caps.Get("analytics"); present {
			t.Fatal("a camera that lists no topics must not get an analytics set")
		}
	}
}

func TestFindStreamInfo(t *testing.T) {
	hosts := map[string]inventory.CameraStreamHostInfo{
		"portones": {Host: "10.1.1.220", Username: "admin", Password: "passPortones"},
		"Patio":    {Host: "10.1.1.224", Username: "admin", Password: "passPatio"},
	}

	t.Run("matches exact name", func(t *testing.T) {
		info := findStreamInfo(hosts, "Patio")
		if info.Host != "10.1.1.224" || info.Password != "passPatio" {
			t.Fatalf("info = %+v, want Patio", info)
		}
	})

	t.Run("matches case-insensitively", func(t *testing.T) {
		info := findStreamInfo(hosts, "Portones")
		if info.Host != "10.1.1.220" || info.Password != "passPortones" {
			t.Fatalf("info = %+v, want portones", info)
		}
	})

	t.Run("matches fallback name", func(t *testing.T) {
		info := findStreamInfo(hosts, "NonExistent", "patio")
		if info.Host != "10.1.1.224" || info.Password != "passPatio" {
			t.Fatalf("info = %+v, want patio", info)
		}
	})

	t.Run("returns empty when not found", func(t *testing.T) {
		info := findStreamInfo(hosts, "Unknown")
		if info.Host != "" || info.Username != "" || info.Password != "" {
			t.Fatalf("info = %+v, want empty", info)
		}
	})
}
