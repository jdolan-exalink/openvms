package camconfig

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

// videoReadTimeout bounds the ONVIF round trips of one model read: a slow camera must not make
// the whole configuration slow.
const videoReadTimeout = 8 * time.Second

// sourceONVIF names the model source that tells whether the camera's video could be read.
const sourceONVIF = "onvif"

// ProfileTokens finds the ONVIF profiles that carry the primary and the detection streams from
// the media profiles stored by the ONVIF probe: the profile whose stream URI is the probe's main
// (sub) stream URL. A stream without exactly one such profile stays empty, and so does a
// detection stream that is the same as the primary one: nothing is guessed.
func ProfileTokens(in Inventory) (primary, detection string) {
	var caps struct {
		Main     string `json:"main_stream_url"`
		Sub      string `json:"sub_stream_url"`
		Profiles []struct {
			Token string `json:"token"`
			URI   string `json:"stream_uri"`
		} `json:"media_profiles"`
	}
	if len(in.ONVIFCapabilities) == 0 || json.Unmarshal(in.ONVIFCapabilities, &caps) != nil {
		return "", ""
	}
	find := func(uri string) string {
		if uri == "" {
			return ""
		}
		token := ""
		for _, p := range caps.Profiles {
			if p.URI != uri || p.Token == "" {
				continue
			}
			if token != "" && token != p.Token {
				return ""
			}
			token = p.Token
		}
		return token
	}
	primary, detection = find(caps.Main), find(caps.Sub)
	if detection == primary {
		detection = ""
	}
	return primary, detection
}

// enrichVideo fills what Frigate does not know about the streams from the camera itself: the
// primary stream's resolution, FPS, codec, bitrate and GOP, and for the detection stream only
// the values still missing (Frigate's detect size and rate describe the detect pipeline and win).
// It never invents values and never fails the read: the outcome is reported as the "onvif"
// source, whose detail is a reason code when the camera could not be read.
func enrichVideo(ctx context.Context, cam *Camera, link *VideoLink) {
	if link == nil {
		return
	}
	if link.Device == nil {
		cam.Sources = append(cam.Sources, Source{Name: sourceONVIF, Status: SourceUnavailable, Detail: link.reason()})
		return
	}
	ctx, cancel := context.WithTimeout(ctx, videoReadTimeout)
	defer cancel()
	streams, err := link.Device.Streams(ctx)
	if err != nil {
		cam.Sources = append(cam.Sources, Source{Name: sourceONVIF, Status: SourceUnavailable, Detail: reasonFor(err)})
		return
	}
	cam.Sources = append(cam.Sources, Source{Name: sourceONVIF, Status: SourceOK})
	if cam.Streams.Primary != nil {
		if ps, ok := findStream(streams, link.PrimaryToken); ok && link.PrimaryToken != "" {
			fillStream(cam.Streams.Primary, ps, true)
		}
	}
	if cam.Streams.Detection != nil {
		if ps, ok := findStream(streams, link.DetectionToken); ok && link.DetectionToken != "" {
			fillStream(cam.Streams.Detection, ps, false)
		}
	}
}

func fillStream(s *Stream, ps onvif.ProfileStream, authoritative bool) {
	if authoritative || s.Width == 0 || s.Height == 0 {
		if ps.Resolution.Width > 0 && ps.Resolution.Height > 0 {
			s.Width, s.Height = ps.Resolution.Width, ps.Resolution.Height
		}
	}
	if (authoritative || s.FPS == 0) && ps.FPS > 0 {
		s.FPS = ps.FPS
	}
	if s.Codec == "" && ps.Codec != "" {
		s.Codec = strings.ToLower(onvif.CanonicalCodec(ps.Codec))
	}
	if ps.BitrateKbps > 0 {
		s.BitrateKbps = ps.BitrateKbps
	}
	if ps.GOP > 0 {
		s.GOP = ps.GOP
	}
}

// VideoOptionsReport is what a camera supports for each stream, for the video-options endpoint.
type VideoOptionsReport struct {
	CameraID  string               `json:"camera_id"`
	Available bool                 `json:"available"`
	Reason    string               `json:"reason,omitempty"`
	Streams   []StreamVideoOptions `json:"streams"`
}

// StreamVideoOptions is the current video setup and the supported values of one stream.
type StreamVideoOptions struct {
	Role         string            `json:"role"`
	ProfileToken string            `json:"profile_token,omitempty"`
	ProfileName  string            `json:"profile_name,omitempty"`
	Available    bool              `json:"available"`
	Reason       string            `json:"reason,omitempty"`
	Current      *VideoSettings    `json:"current,omitempty"`
	Codecs       []CodecVideoLimit `json:"codecs"`
	ApplyVia     map[string]string `json:"apply_via"`
}

// VideoSettings are the values a stream uses now.
type VideoSettings struct {
	Codec       string  `json:"codec,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
	BitrateKbps int     `json:"bitrate_kbps,omitempty"`
	BitrateMode string  `json:"bitrate_mode,omitempty"`
	GOP         int     `json:"gop,omitempty"`
}

// CodecVideoLimit is what the device declares for one codec; an absent item was not reported.
type CodecVideoLimit struct {
	Codec        string            `json:"codec"`
	Resolutions  []VideoResolution `json:"resolutions"`
	FrameRates   []float64         `json:"frame_rates"`
	FPS          *FloatLimit       `json:"fps,omitempty"`
	BitrateKbps  *IntLimit         `json:"bitrate_kbps,omitempty"`
	GOP          *IntLimit         `json:"gop,omitempty"`
	BitrateModes []string          `json:"bitrate_modes"`
}

// IntLimit and FloatLimit are inclusive ranges.
type IntLimit struct {
	Min int `json:"min"`
	Max int `json:"max"`
}
type FloatLimit struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// BuildVideoOptions asks the camera for the current settings and supported values of the primary
// and detection streams. It degrades instead of failing: a stream that cannot be read is listed
// with available=false and a reason code.
func BuildVideoOptions(ctx context.Context, cameraID string, link *VideoLink) VideoOptionsReport {
	rep := VideoOptionsReport{CameraID: cameraID, Streams: []StreamVideoOptions{}}
	var streams []onvif.ProfileStream
	devReason := ""
	switch {
	case link == nil || link.Device == nil:
		devReason = link.reason()
	default:
		cctx, cancel := context.WithTimeout(ctx, videoReadTimeout)
		defer cancel()
		var err error
		if streams, err = link.Device.Streams(cctx); err != nil {
			devReason = reasonFor(err)
		}
	}
	for _, role := range []string{StreamPrimary, StreamDetection} {
		so := StreamVideoOptions{Role: role, Codecs: []CodecVideoLimit{}, ApplyVia: applyVia(link, role, devReason == "")}
		token := link.token(role)
		switch {
		case devReason != "":
			so.Reason = devReason
		case token == "":
			so.Reason = VideoUnsupported
		default:
			so = fillStreamOptions(ctx, link, so, streams, token)
		}
		if so.Available {
			rep.Available = true
		}
		rep.Streams = append(rep.Streams, so)
	}
	if !rep.Available {
		rep.Reason = devReason
		if rep.Reason == "" {
			rep.Reason = VideoUnsupported
		}
	}
	return rep
}

func fillStreamOptions(ctx context.Context, link *VideoLink, so StreamVideoOptions, streams []onvif.ProfileStream, token string) StreamVideoOptions {
	ps, ok := findStream(streams, token)
	if !ok {
		so.Reason = VideoUnsupported
		return so
	}
	so.ProfileToken, so.ProfileName = ps.Token, ps.Name
	so.Current = &VideoSettings{
		Codec: strings.ToLower(onvif.CanonicalCodec(ps.Codec)), Width: ps.Resolution.Width, Height: ps.Resolution.Height,
		FPS: ps.FPS, BitrateKbps: ps.BitrateKbps, BitrateMode: ps.BitrateMode, GOP: ps.GOP,
	}
	cctx, cancel := context.WithTimeout(ctx, videoReadTimeout)
	defer cancel()
	opts, err := link.Device.Options(cctx, token)
	if err != nil {
		so.Reason = reasonFor(err)
		return so
	}
	for _, c := range opts.Codecs {
		lim := CodecVideoLimit{Codec: strings.ToLower(c.Codec), Resolutions: []VideoResolution{}, FrameRates: append([]float64{}, c.FrameRates...), BitrateModes: append([]string{}, c.BitrateModes...)}
		for _, r := range c.Resolutions {
			lim.Resolutions = append(lim.Resolutions, VideoResolution{Width: r.Width, Height: r.Height})
		}
		if c.FPS.Max > 0 {
			lim.FPS = &FloatLimit{Min: c.FPS.Min, Max: c.FPS.Max}
		}
		if c.Bitrate.Max > 0 {
			lim.BitrateKbps = &IntLimit{Min: c.Bitrate.Min, Max: c.Bitrate.Max}
		}
		if c.GOP.Max > 0 {
			lim.GOP = &IntLimit{Min: c.GOP.Min, Max: c.GOP.Max}
		}
		so.Codecs = append(so.Codecs, lim)
	}
	if len(so.Codecs) == 0 {
		so.Reason = VideoUnsupported
		return so
	}
	so.Available = true
	return so
}

// applyVia says, per field, which path a change of the stream takes (see VideoPatch).
func applyVia(link *VideoLink, role string, deviceUp bool) map[string]string {
	via := ViaNone
	if deviceUp && link.token(role) != "" {
		via = ViaONVIF
	}
	out := map[string]string{"resolution": via, "fps": via, "codec": via, "bitrate_kbps": via, "gop": via}
	if role == StreamDetection && link.token(role) == "" {
		out["resolution"], out["fps"] = ViaFrigate, ViaFrigate
	}
	return out
}
