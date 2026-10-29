package inventory

import (
	"context"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

func TestExtractCameraStreamHosts(t *testing.T) {
	rawPaths := map[string]any{
		"go2rtc": map[string]any{
			// #nosec G101 -- test dummy credentials in fixtures.
			"streams": map[string]any{
				"Portones_main": "rtsp://admin:Daytona1309@10.1.1.220:554#video=copy",
				"Patio": []any{
					"ffmpeg:Patio_main#video=h264",
					"rtsp://admin:123456@10.1.1.224:554/profile1#video=copy",
				},
				"Cochera_sub":          "rtsp://admin:Daytona1309*@10.1.1.225:554/profile1",
				"LocalRestream":        "rtsp://127.0.0.1:8554/cam",
				"avigilon_id549_sub_1": "rtsp://admin:avigilonpass@172.22.148.57:554/default",
			},
		},
		"cameras": map[string]any{
			"Baldio_Fondo": map[string]any{
				"onvif": map[string]any{
					"host":     "10.1.1.226",
					"user":     "admin",
					"password": "secretpassword",
				},
			},
			"Camara_ID_549_1": map[string]any{
				"ffmpeg": map[string]any{
					"inputs": []any{
						map[string]any{
							"path": "rtsp://127.0.0.1:8554/avigilon_id549_sub_1",
						},
					},
				},
			},
			"Quincho": map[string]any{
				"ffmpeg": map[string]any{
					"inputs": []any{
						map[string]any{
							// #nosec G101 -- test dummy credentials in fixtures.
							"path": "rtsp://admin:daytona1309@10.1.1.227:554/Streaming/channels/101",
						},
					},
				},
			},
			"Portones": map[string]any{
				"ffmpeg": map[string]any{
					"inputs": []any{
						map[string]any{
							"path": "rtsp://127.0.0.1:8554/Portones",
						},
					},
				},
			},
		},
	}

	hosts := ExtractCameraStreamHosts(rawPaths)

	// Portones
	p, ok := hosts["Portones"]
	if !ok {
		t.Fatalf("Portones not found in hosts")
	}
	if p.Host != "10.1.1.220" || p.Username != "admin" || p.Password != "Daytona1309" {
		t.Errorf("Portones got %+v, want 10.1.1.220/admin/Daytona1309", p)
	}

	// Lowercase portones
	pLower, ok := hosts["portones"]
	if !ok || pLower.Host != "10.1.1.220" {
		t.Errorf("lowercase portones got %+v, want 10.1.1.220", pLower)
	}

	// Patio from []any
	patio, ok := hosts["Patio"]
	if !ok {
		t.Fatalf("Patio not found in hosts")
	}
	if patio.Host != "10.1.1.224" || patio.Username != "admin" || patio.Password != "123456" {
		t.Errorf("Patio got %+v, want 10.1.1.224/admin/123456", patio)
	}

	// Cochera with special character in password
	cochera, ok := hosts["Cochera"]
	if !ok {
		t.Fatalf("Cochera not found in hosts")
	}
	if cochera.Host != "10.1.1.225" || cochera.Username != "admin" || cochera.Password != "Daytona1309*" {
		t.Errorf("Cochera got %+v, want 10.1.1.225/admin/Daytona1309*", cochera)
	}

	// Baldio_Fondo from onvif block
	baldio, ok := hosts["Baldio_Fondo"]
	if !ok {
		t.Fatalf("Baldio_Fondo not found in hosts")
	}
	if baldio.Host != "10.1.1.226" || baldio.Username != "admin" || baldio.Password != "secretpassword" {
		t.Errorf("Baldio_Fondo got %+v, want 10.1.1.226/admin/secretpassword", baldio)
	}

	// Quincho from ffmpeg.inputs
	quincho, ok := hosts["Quincho"]
	if !ok {
		t.Fatalf("Quincho not found in hosts")
	}
	if quincho.Host != "10.1.1.227" || quincho.Username != "admin" || quincho.Password != "daytona1309" {
		t.Errorf("Quincho got %+v, want 10.1.1.227/admin/daytona1309", quincho)
	}

	// Camara_ID_549_1 dereferenced from go2rtc stream avigilon_id549_sub_1
	c549, ok := hosts["Camara_ID_549_1"]
	if !ok {
		t.Fatalf("Camara_ID_549_1 not found in hosts")
	}
	if c549.Host != "172.22.148.57" || c549.Username != "admin" || c549.Password != "avigilonpass" {
		t.Errorf("Camara_ID_549_1 got %+v, want 172.22.148.57/admin/avigilonpass", c549)
	}

	// LocalRestream should not be present as an external host
	if local, ok := hosts["LocalRestream"]; ok && local.Host != "" {
		t.Errorf("LocalRestream should have empty host, got %+v", local)
	}
}

func TestDisplayNameBirdEye(t *testing.T) {
	if got := displayName("birdseye"); got != "BirdEye" {
		t.Fatalf("expected BirdEye, got %q", got)
	}
	if got := displayName("BIRDSEYE"); got != "BirdEye" {
		t.Fatalf("expected BirdEye, got %q", got)
	}
	if got := displayName("acceso_norte"); got != "Acceso norte" {
		t.Fatalf("expected Acceso norte, got %q", got)
	}
}

type mockAdapterForDiscover struct {
	frigate.Adapter
}

func (m *mockAdapterForDiscover) Capabilities(_ context.Context) (frigate.Capabilities, error) {
	return frigate.Capabilities{}, nil
}

func (m *mockAdapterForDiscover) ListCameras(_ context.Context) ([]frigate.Camera, error) {
	return []frigate.Camera{{Name: "acceso", Enabled: true, LiveStream: "acceso_sub", HQStream: "acceso_main"}}, nil
}

func (m *mockAdapterForDiscover) Version() string { return "0.17.0" }
func (m *mockAdapterForDiscover) Name() string    { return "v017" }

func TestDiscoverIncludesBirdEyeChannel(t *testing.T) {
	s := &Service{
		Connect: func(_ context.Context, _ frigate.ConnInfo) (frigate.Adapter, error) {
			return &mockAdapterForDiscover{}, nil
		},
	}
	res, err := s.discover(context.Background(), frigate.ConnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	var foundBirdseye bool
	for _, cam := range res.Cameras {
		if cam.Name == "birdseye" {
			foundBirdseye = true
			if cam.LiveStream != "birdseye" || cam.HQStream != "birdseye" || !cam.Enabled {
				t.Fatalf("unexpected birdseye camera: %+v", cam)
			}
		}
	}
	if !foundBirdseye {
		t.Fatalf("expected birdseye camera in discovered cameras: %+v", res.Cameras)
	}
}

func TestExtractCameraStreamHostsRedactedCredentialsIgnored(t *testing.T) {
	rawPaths := map[string]any{
		"go2rtc": map[string]any{
			"streams": map[string]any{
				"Escuela_main": "rtsp://*:*@10.0.15.107:554/profile1#video=copy",
			},
		},
		"cameras": map[string]any{
			"Escuela": map[string]any{
				"onvif": map[string]any{
					"host":     "10.0.15.107",
					"user":     "*",
					"password": "*****",
				},
			},
		},
	}

	hosts := ExtractCameraStreamHosts(rawPaths)
	e, ok := hosts["Escuela"]
	if !ok {
		t.Fatalf("Escuela not found in hosts")
	}
	if e.Host != "10.0.15.107" {
		t.Errorf("expected host 10.0.15.107, got %q", e.Host)
	}
	if e.Username != "" {
		t.Errorf("expected redacted username to be discarded, got %q", e.Username)
	}
	if e.Password != "" {
		t.Errorf("expected redacted password to be discarded, got %q", e.Password)
	}
}
