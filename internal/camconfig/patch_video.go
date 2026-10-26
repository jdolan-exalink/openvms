package camconfig

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

// Roles of the streams a patch can change.
const (
	StreamPrimary   = "primary"
	StreamDetection = "detection"
)

// Reasons video settings cannot be read or changed. They are codes for clients, not texts.
const (
	VideoNoONVIF      = "no_onvif"
	VideoNoCredential = "no_credentials"
	VideoUnreachable  = "unreachable"
	VideoUnsupported  = "unsupported"
)

// Ways a video field reaches the camera.
const (
	ViaONVIF   = "onvif"
	ViaFrigate = "frigate"
	ViaNone    = "none"
)

// VideoResolution is a width and height in pixels.
type VideoResolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// VideoPatch is a sparse change of one stream's video settings: nil fields stay as they are.
//
// Where each field goes:
//
//	streams.primary.*                    -> ONVIF encoder of the primary profile (SetVideoEncoder)
//	streams.detection.*, own ONVIF profile -> ONVIF encoder of that profile
//	streams.detection.resolution|fps, no own ONVIF profile -> Frigate detect.width/height/fps
//	streams.detection.codec|bitrate_kbps|gop, no own ONVIF profile -> rejected (nothing to write to)
type VideoPatch struct {
	Resolution  *VideoResolution `json:"resolution,omitempty"`
	FPS         *float64         `json:"fps,omitempty"`
	Codec       *string          `json:"codec,omitempty"`
	BitrateKbps *int             `json:"bitrate_kbps,omitempty"`
	GOP         *int             `json:"gop,omitempty"`
}

// StreamsPatch changes the video settings of the primary and detection streams.
type StreamsPatch struct {
	Primary   *VideoPatch `json:"primary,omitempty"`
	Detection *VideoPatch `json:"detection,omitempty"`
}

var videoCodecs = []string{"h264", "h265", "mjpeg", "mpeg4"}

func (v *VideoPatch) empty() bool {
	return v == nil || (v.Resolution == nil && v.FPS == nil && v.Codec == nil && v.BitrateKbps == nil && v.GOP == nil)
}

func streamsEmpty(s *StreamsPatch) bool {
	return s == nil || (s.Primary.empty() && s.Detection.empty())
}

// HasVideo reports whether the patch changes any stream setting.
func (p Patch) HasVideo() bool { return !streamsEmpty(p.Streams) }

func (v *VideoPatch) validate(role string) error {
	if v == nil {
		return nil
	}
	path := "streams." + role
	if r := v.Resolution; r != nil && (r.Width <= 0 || r.Height <= 0 || r.Width > 16384 || r.Height > 16384) {
		return invalid("%s.resolution: width and height must be positive", path)
	}
	if f := v.FPS; f != nil && (*f <= 0 || *f > 240 || math.IsNaN(*f)) {
		return invalid("%s.fps: must be between 0 and 240", path)
	}
	if c := v.Codec; c != nil && !slices.Contains(videoCodecs, strings.ToLower(strings.TrimSpace(*c))) {
		return invalid("%s.codec: %q is not one of h264, h265, mjpeg, mpeg4", path, *c)
	}
	if b := v.BitrateKbps; b != nil && (*b <= 0 || *b > 1_000_000) {
		return invalid("%s.bitrate_kbps: must be positive", path)
	}
	if g := v.GOP; g != nil && (*g <= 0 || *g > 10_000) {
		return invalid("%s.gop: must be positive", path)
	}
	return nil
}

func (s *StreamsPatch) validate() error {
	if s == nil {
		return nil
	}
	if err := s.Primary.validate(StreamPrimary); err != nil {
		return err
	}
	return s.Detection.validate(StreamDetection)
}

func (v *VideoPatch) normalized() *VideoPatch {
	if v.empty() {
		return nil
	}
	out := *v
	if v.Codec != nil {
		c := strings.ToLower(strings.TrimSpace(*v.Codec))
		out.Codec = &c
	}
	return &out
}

func (s *StreamsPatch) normalized() *StreamsPatch {
	if streamsEmpty(s) {
		return nil
	}
	return &StreamsPatch{Primary: s.Primary.normalized(), Detection: s.Detection.normalized()}
}

func mergeVideo(base, over *VideoPatch) *VideoPatch {
	if base.empty() {
		return over.normalized()
	}
	out := *base.normalized()
	if o := over.normalized(); o != nil {
		if o.Resolution != nil {
			out.Resolution = o.Resolution
		}
		if o.FPS != nil {
			out.FPS = o.FPS
		}
		if o.Codec != nil {
			out.Codec = o.Codec
		}
		if o.BitrateKbps != nil {
			out.BitrateKbps = o.BitrateKbps
		}
		if o.GOP != nil {
			out.GOP = o.GOP
		}
	}
	return &out
}

func mergeStreams(base, over *StreamsPatch) *StreamsPatch {
	if streamsEmpty(over) {
		return base.normalized()
	}
	if streamsEmpty(base) {
		return over.normalized()
	}
	return &StreamsPatch{
		Primary:   mergeVideo(base.Primary, over.Primary),
		Detection: mergeVideo(base.Detection, over.Detection),
	}
}

// VideoDevice is the camera's ONVIF video interface. onvif.ConnDevice implements it; the API
// supplies a connection built from the camera's credentials and tests supply fakes.
type VideoDevice interface {
	Streams(ctx context.Context) ([]onvif.ProfileStream, error)
	Options(ctx context.Context, profileToken string) (onvif.VideoOptions, error)
	Set(ctx context.Context, profileToken string, desired onvif.VideoDesired) (onvif.ProfileStream, error)
}

// VideoLink is how a camera's video is reached. Device is nil when it cannot be, and Reason
// (one of the Video* codes) says why. PrimaryToken and DetectionToken are the ONVIF profiles
// that carry the primary and detection streams; empty means unknown or ambiguous (and for the
// detection stream, that it has no profile of its own).
type VideoLink struct {
	Device         VideoDevice
	Reason         string
	PrimaryToken   string
	DetectionToken string
}

// VideoReasonText is the plain-language form of a reason code.
func VideoReasonText(reason string) string { return videoReasonText(reason) }

// videoReasonText is the plain-language form of a reason code.
func videoReasonText(reason string) string {
	switch reason {
	case VideoNoONVIF:
		return "La cámara no tiene ONVIF configurado."
	case VideoNoCredential:
		return "Faltan credenciales ONVIF o no tiene permiso para usarlas."
	case VideoUnreachable:
		return "No se pudo consultar la cámara por ONVIF."
	default:
		return "La cámara no permite consultar ni cambiar el video por ONVIF."
	}
}

// reasonFor maps an error of the video interface to a reason code. Nothing of the error text
// is kept: it could quote device responses.
func reasonFor(err error) string {
	if errors.Is(err, onvif.ErrProfileNotFound) || errors.Is(err, onvif.ErrNoVideoEncoder) {
		return VideoUnsupported
	}
	return VideoUnreachable
}

func (l *VideoLink) reason() string {
	switch {
	case l == nil:
		return VideoNoONVIF
	case l.Reason != "":
		return l.Reason
	default:
		return VideoUnsupported
	}
}

func roleLabel(role string) string {
	if role == StreamDetection {
		return "detección"
	}
	return "principal"
}

func resText(r onvif.VideoResolution) string { return fmt.Sprintf("%d×%d", r.Width, r.Height) }

// videoPlan is what the video part of a patch adds to a review.
type videoPlan struct {
	changes     []Change
	unsupported []string
	impact      []ImpactItem
	// detect is true when the patch reaches Frigate's detect section.
	detect bool
}

// frigateDetect reports whether the detection stream's resolution and FPS go to Frigate: it
// has no ONVIF profile of its own.
func (l *VideoLink) frigateDetect() bool { return l == nil || l.DetectionToken == "" }

func (l *VideoLink) token(role string) string {
	if l == nil {
		return ""
	}
	if role == StreamDetection {
		return l.DetectionToken
	}
	return l.PrimaryToken
}

func toDesired(v *VideoPatch, current string) onvif.VideoDesired {
	d := onvif.VideoDesired{FPS: v.FPS, BitrateKbps: v.BitrateKbps, GOP: v.GOP, CurrentCodec: current}
	if v.Codec != nil {
		d.Codec = *v.Codec
	}
	if v.Resolution != nil {
		d.Resolution = &onvif.VideoResolution{Width: v.Resolution.Width, Height: v.Resolution.Height}
	}
	return d
}

// planVideo reads the device (read only) to validate and describe the video part of a patch.
func (a FrigateAdapter) planVideo(ctx context.Context, current Camera, s *StreamsPatch) videoPlan {
	var out videoPlan
	if streamsEmpty(s) {
		return out
	}
	var streams []onvif.ProfileStream
	var streamsErr string
	if a.Video != nil && a.Video.Device != nil {
		var err error
		if streams, err = a.Video.Device.Streams(ctx); err != nil {
			streamsErr = reasonFor(err)
		}
	}
	restart := false
	for _, role := range []string{StreamPrimary, StreamDetection} {
		vp := s.Primary
		if role == StreamDetection {
			vp = s.Detection
		}
		if vp.empty() {
			continue
		}
		label := roleLabel(role)
		reject := func(format string, args ...any) {
			out.unsupported = append(out.unsupported, fmt.Sprintf("Stream %s: ", label)+fmt.Sprintf(format, args...))
		}
		token := a.Video.token(role)
		if token == "" && role == StreamDetection && a.Video.frigateDetect() {
			out.planFrigateDetect(current.Streams.Detection, vp, reject)
			continue
		}
		switch {
		case a.Video == nil || a.Video.Device == nil:
			reject("%s", videoReasonText(a.Video.reason()))
			continue
		case token == "":
			reject("no se pudo identificar su perfil ONVIF.")
			continue
		case streamsErr != "":
			reject("%s", videoReasonText(streamsErr))
			continue
		}
		cur, ok := findStream(streams, token)
		if !ok {
			reject("%s", videoReasonText(VideoUnsupported))
			continue
		}
		opts, err := a.Video.Device.Options(ctx, token)
		if err != nil {
			reject("%s", videoReasonText(reasonFor(err)))
			continue
		}
		desired := toDesired(vp, cur.Codec)
		for _, v := range onvif.ValidateVideoChange(opts, desired) {
			reject("%s", v.Message)
		}
		out.changes = append(out.changes, videoChanges(role, label, cur, vp)...)
		if len(onvif.ClassifyImpact(cur, desired)) > 0 {
			restart = true
		}
	}
	if restart {
		out.impact = append(out.impact, ImpactItem{Code: ImpactStreamRestart, Message: onvif.StreamRestartMessage})
	}
	return out
}

func findStream(streams []onvif.ProfileStream, token string) (onvif.ProfileStream, bool) {
	for _, s := range streams {
		if s.Token == token {
			return s, true
		}
	}
	return onvif.ProfileStream{}, false
}

func videoChanges(role, label string, cur onvif.ProfileStream, v *VideoPatch) []Change {
	var out []Change
	add := func(field, name string, from, to any) {
		out = append(out, Change{Path: "streams." + role + "." + field, Label: name + " del stream " + label, From: from, To: to})
	}
	nz := func(n int) any {
		if n == 0 {
			return nil
		}
		return n
	}
	if r := v.Resolution; r != nil && *r != (VideoResolution{Width: cur.Resolution.Width, Height: cur.Resolution.Height}) {
		var from any
		if cur.Resolution.Width > 0 {
			from = resText(cur.Resolution)
		}
		add("resolution", "resolución", from, resText(onvif.VideoResolution{Width: r.Width, Height: r.Height}))
	}
	if f := v.FPS; f != nil && math.Abs(*f-cur.FPS) > 1e-6 {
		var from any
		if cur.FPS > 0 {
			from = cur.FPS
		}
		add("fps", "FPS", from, *f)
	}
	if c := v.Codec; c != nil && onvif.CanonicalCodec(*c) != onvif.CanonicalCodec(cur.Codec) {
		var from any
		if cur.Codec != "" {
			from = strings.ToLower(onvif.CanonicalCodec(cur.Codec))
		}
		add("codec", "códec", from, *c)
	}
	if b := v.BitrateKbps; b != nil && *b != cur.BitrateKbps {
		add("bitrate_kbps", "bitrate (kbps)", nz(cur.BitrateKbps), *b)
	}
	if g := v.GOP; g != nil && *g != cur.GOP {
		add("gop", "GOP", nz(cur.GOP), *g)
	}
	return out
}

// planFrigateDetect describes the detection changes Frigate owns (detect.width/height/fps).
func (p *videoPlan) planFrigateDetect(cur *Stream, v *VideoPatch, reject func(string, ...any)) {
	if v.Codec != nil || v.BitrateKbps != nil || v.GOP != nil {
		reject("el códec, el bitrate y el GOP solo se cambian en la cámara y no tiene un perfil ONVIF propio para la detección.")
	}
	var c Stream
	if cur != nil {
		c = *cur
	}
	if r := v.Resolution; r != nil && (r.Width != c.Width || r.Height != c.Height) {
		var from any
		if c.Width > 0 {
			from = fmt.Sprintf("%d×%d", c.Width, c.Height)
		}
		p.changes = append(p.changes, Change{Path: "streams.detection.resolution", Label: "resolución del stream detección", From: from, To: fmt.Sprintf("%d×%d", r.Width, r.Height)})
		p.detect = true
	}
	if f := v.FPS; f != nil {
		if *f != math.Trunc(*f) {
			reject("los FPS de detección deben ser un número entero.")
		} else if *f != c.FPS {
			var from any
			if c.FPS > 0 {
				from = c.FPS
			}
			p.changes = append(p.changes, Change{Path: "streams.detection.fps", Label: "FPS del stream detección", From: from, To: *f})
			p.detect = true
		}
	}
}

// detectSection is the part of Frigate's detect section the video patch writes.
func (a FrigateAdapter) detectSection(s *StreamsPatch) map[string]any {
	if s == nil || s.Detection.empty() || !a.Video.frigateDetect() {
		return nil
	}
	out := map[string]any{}
	if r := s.Detection.Resolution; r != nil {
		out["width"], out["height"] = r.Width, r.Height
	}
	if f := s.Detection.FPS; f != nil {
		out["fps"] = int(*f)
	}
	return out
}

// applyVideo writes the ONVIF part of a patch. Every error is reduced to a message safe to
// store and show: it never carries device responses, addresses or credentials.
func (a FrigateAdapter) applyVideo(ctx context.Context, s *StreamsPatch) error {
	if streamsEmpty(s) {
		return nil
	}
	for _, role := range []string{StreamPrimary, StreamDetection} {
		vp := s.Primary
		if role == StreamDetection {
			vp = s.Detection
		}
		token := a.Video.token(role)
		if vp.empty() || (role == StreamDetection && token == "" && a.Video.frigateDetect()) {
			continue
		}
		label := roleLabel(role)
		if a.Video == nil || a.Video.Device == nil || token == "" {
			return invalid("stream %s: %s", label, videoReasonText(a.Video.reason()))
		}
		if _, err := a.Video.Device.Set(ctx, token, toDesired(vp, "")); err != nil {
			return videoApplyError(label, err)
		}
	}
	return nil
}

func videoApplyError(label string, err error) error {
	var ve *onvif.ValidationError
	var verr *onvif.VerifyError
	switch {
	case errors.As(err, &ve):
		msgs := make([]string, 0, len(ve.Violations))
		for _, v := range ve.Violations {
			msgs = append(msgs, v.Message)
		}
		return &ValidationError{Msg: fmt.Sprintf("stream %s: la cámara no soporta el cambio: %s", label, strings.Join(msgs, " "))}
	case errors.As(err, &verr):
		return fmt.Errorf("stream %s: la cámara no aplicó el cambio (%s)", label, strings.Join(verr.Fields, ", "))
	case errors.Is(err, onvif.ErrProfileNotFound), errors.Is(err, onvif.ErrNoVideoEncoder), errors.Is(err, onvif.ErrGOPNotWritable):
		return fmt.Errorf("stream %s: la cámara no permite cambiar este parámetro", label)
	default:
		return fmt.Errorf("stream %s: no se pudo aplicar el cambio de video en la cámara", label)
	}
}
