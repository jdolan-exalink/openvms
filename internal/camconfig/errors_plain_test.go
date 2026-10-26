package camconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestPlainFromTextCatalog(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"ffmpeg exited with code 1 (record process)", "No se pudo iniciar el stream de grabación."},
		{"detect: ffmpeg exited with code 1", "No se pudo iniciar el stream de detección."},
		{"ffmpeg exited with code 1", "No se pudo iniciar el stream de la cámara."},
		{"Server returned 401 Unauthorized", "La cámara o el servidor rechazó las credenciales."},
		{"dial tcp 10.0.0.5:80: connect: connection refused", "No se pudo conectar: el dispositivo o el servidor no responde."},
		{"Post: context deadline exceeded", "No se pudo conectar: el dispositivo o el servidor no responde."},
		{"x509: certificate signed by unknown authority", "No se pudo establecer una conexión segura."},
		{"lookup cam.local: no such host", "No se encontró la dirección configurada."},
		{"SOAP fault: ter:ActionNotSupported", "El dispositivo no admite este cambio."},
		{"SOAP fault: ter:InvalidArgVal", "El dispositivo no acepta ese valor."},
		{"onvif: unexpected soap envelope", "La cámara rechazó el cambio por ONVIF."},
		{"el motor no refleja el cambio aplicado", "El motor no reflejó el cambio aplicado."},
		{"1 validation error for CameraConfig: Extra inputs are not permitted", "El motor rechazó la configuración."},
		{"something odd", "No se pudo completar el cambio."},
	}
	for _, c := range cases {
		if got := PlainFromText(c.raw); got.Message != c.want {
			t.Errorf("%q -> %q, want %q", c.raw, got.Message, c.want)
		}
	}
}

func TestPlainErrorNeverCarriesRawFfmpegOrSecrets(t *testing.T) {
	raw := "ffmpeg exited with code 1 for rtsp://" + testUserinfo + "10.0.0.5/stream?token=abc password=hunter2"
	got := PlainFromText(raw)
	if strings.Contains(got.Message, "ffmpeg") || strings.Contains(got.Message, "code 1") {
		t.Fatalf("message is technical: %q", got.Message)
	}
	for _, leak := range []string{"hunter2", "token=abc"} {
		if strings.Contains(got.Technical, leak) {
			t.Fatalf("technical leaks %q: %q", leak, got.Technical)
		}
	}
	if !strings.Contains(got.Technical, "ffmpeg exited with code 1") {
		t.Fatalf("technical keeps the ffmpeg text for the details view: %q", got.Technical)
	}
}

func TestPlainFromError(t *testing.T) {
	if got := PlainFromError(nil); got != (PlainError{}) {
		t.Fatalf("nil -> %+v", got)
	}
	got := PlainFromError(&ValidationError{Msg: "stream Principal: la cámara no permite cambiar este parámetro"})
	if got.Message != "stream Principal: la cámara no permite cambiar este parámetro" || got.Technical != "" {
		t.Fatalf("validation error is already plain: %+v", got)
	}
	if got := PlainFromError(errors.New("connection refused")); got.Message == "" || got.Technical != "connection refused" {
		t.Fatalf("generic: %+v", got)
	}
}
