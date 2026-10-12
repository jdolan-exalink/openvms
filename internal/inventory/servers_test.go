package inventory

import (
	"testing"
)

func TestExtractCameraStreamHosts(t *testing.T) {
	rawPaths := map[string]any{
		"go2rtc": map[string]any{
			"streams": map[string]any{
				"Portones_main": "rtsp://admin:Daytona1309@10.1.1.220:554#video=copy",
				"Patio": []any{
					"ffmpeg:Patio_main#video=h264",
					"rtsp://admin:123456@10.1.1.224:554/profile1#video=copy",
				},
				"Cochera_sub": "rtsp://admin:Daytona1309*@10.1.1.225:554/profile1",
				"LocalRestream": "rtsp://127.0.0.1:8554/cam",
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
			"Quincho": map[string]any{
				"ffmpeg": map[string]any{
					"inputs": []any{
						map[string]any{
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

	// LocalRestream should not be present as an external host
	if local, ok := hosts["LocalRestream"]; ok && local.Host != "" {
		t.Errorf("LocalRestream should have empty host, got %+v", local)
	}
}
