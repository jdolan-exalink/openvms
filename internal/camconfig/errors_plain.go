package camconfig

import (
	"errors"
	"regexp"
	"strings"
)

// PlainError is a failure in words for the person using the screen (Message) next to the
// technical text for "Ver información técnica" (Technical). Technical is sanitized: no
// credentials, query strings or key=value secrets (see SanitizeError). PRD §42: the screen
// says "No se pudo iniciar el stream de grabación.", never `ffmpeg exited with code 1`.
type PlainError struct {
	Message   string `json:"message"`
	Technical string `json:"technical"`
}

// Fallback messages when nothing in the catalog matches.
const (
	plainUnknown = "No se pudo completar el cambio."
)

type plainRule struct {
	re      *regexp.Regexp
	message string
}

// plainRules is evaluated in order; the first match wins, so specific rules come first.
var plainRules = []plainRule{
	{regexp.MustCompile(`(?i)ffmpeg.*(record|segment|clip)|(record|segment).*ffmpeg`), "No se pudo iniciar el stream de grabación."},
	{regexp.MustCompile(`(?i)ffmpeg.*detect|detect.*ffmpeg`), "No se pudo iniciar el stream de detección."},
	{regexp.MustCompile(`(?i)ffmpeg|avformat|libav|rtsp.*(error|fail)|moov atom|invalid data found`), "No se pudo iniciar el stream de la cámara."},
	{regexp.MustCompile(`(?i)\b401\b|unauthori[sz]ed|notauthorized|authentication failed|wrong (user|password|credentials)|invalid credentials|sender not authorized`), "La cámara o el servidor rechazó las credenciales."},
	{regexp.MustCompile(`(?i)\b403\b|forbidden`), "No hay permiso para realizar este cambio en el dispositivo."},
	{regexp.MustCompile(`(?i)x509|certificate|tls:|handshake`), "No se pudo establecer una conexión segura."},
	{regexp.MustCompile(`(?i)no such host|lookup .*(fail|no such)|name resolution|dns`), "No se encontró la dirección configurada."},
	{regexp.MustCompile(`(?i)connection refused|no route to host|network is unreachable|i/o timeout|deadline exceeded|timed out|timeout|connection reset|\beof\b|dial tcp`), "No se pudo conectar: el dispositivo o el servidor no responde."},
	{regexp.MustCompile(`(?i)actionnotsupported|optionalactionnotsupported|not supported|unsupported`), "El dispositivo no admite este cambio."},
	{regexp.MustCompile(`(?i)invalidargval|invalid argument|argument.*(invalid|out of range)|out of range`), "El dispositivo no acepta ese valor."},
	{regexp.MustCompile(`(?i)\bonvif\b|soap|\bter:|\bwsdl\b|s:fault|env:fault`), "La cámara rechazó el cambio por ONVIF."},
	{regexp.MustCompile(`(?i)no refleja el cambio|read-back|did not match`), "El motor no reflejó el cambio aplicado."},
	{regexp.MustCompile(`(?i)validation error|extra inputs|field required|not a valid|input should|value error|invalid config|config(uration)? (is )?invalid`), "El motor rechazó la configuración."},
	{regexp.MustCompile(`(?i)\b404\b|not found`), "No se encontró el recurso en el motor o el dispositivo."},
	{regexp.MustCompile(`(?i)\b50[0-9]\b|internal server error|bad gateway|service unavailable`), "El motor respondió con un error interno."},
}

// PlainFromText maps raw engine, ffmpeg or ONVIF text to a plain message plus its sanitized
// technical text. An empty input yields the zero value.
func PlainFromText(raw string) PlainError {
	if strings.TrimSpace(raw) == "" {
		return PlainError{}
	}
	tech := SanitizeError(errors.New(raw))
	for _, r := range plainRules {
		if r.re.MatchString(raw) {
			return PlainError{Message: r.message, Technical: tech}
		}
	}
	return PlainError{Message: plainUnknown, Technical: tech}
}

// PlainFromError is PlainFromText for an error. A *ValidationError already speaks to the
// person (its text is authored by OpenVMS), so it is the message and has no technical part.
func PlainFromError(err error) PlainError {
	if err == nil {
		return PlainError{}
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return PlainError{Message: ve.Msg}
	}
	return PlainFromText(err.Error())
}
