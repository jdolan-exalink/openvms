package camconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// FrigateEngine is the part of a Frigate adapter the model needs; frigate.Adapter satisfies it.
type FrigateEngine interface {
	Version() string
	// CameraConfig returns the effective camera section of Frigate's config as generic JSON.
	CameraConfig(ctx context.Context, camera string, secrets bool) (map[string]any, error)
	// StreamCodecs maps go2rtc stream names to their video codec.
	StreamCodecs(ctx context.Context) (map[string]string, error)
}

// FrigateAdapter builds the model from the inventory row, Frigate's camera document and the
// go2rtc stream codecs. Engine may be nil when the server could not be reached: the model
// then carries inventory data only.
type FrigateAdapter struct {
	Engine FrigateEngine
	// Writer applies section patches; nil means the adapter can only read.
	Writer SectionWriter
	// Video reaches the camera's ONVIF video settings: it enriches the streams on Read and
	// applies stream patches. Nil means the camera's own video is not consulted.
	Video *VideoLink
}

var _ Adapter = FrigateAdapter{}

// Read implements Adapter.
func (a FrigateAdapter) Read(ctx context.Context, in Inventory) (Camera, error) {
	if in.CameraID == "" {
		return Camera{}, fmt.Errorf("camconfig: camera id is required")
	}
	cam := baseModel(in)
	cam.Origin.Engine = "frigate"

	var doc map[string]any
	var codecs map[string]string
	if a.Engine == nil {
		cam.Sources = append(cam.Sources, unavailable("frigate"))
	} else {
		cam.Origin.EngineVersion = a.Engine.Version()
		var err error
		if doc, err = a.Engine.CameraConfig(ctx, in.RemoteName, false); err != nil {
			doc = nil
			cam.Sources = append(cam.Sources, unavailable("frigate"))
		} else {
			cam.Sources = append(cam.Sources, Source{Name: "frigate", Status: SourceOK})
		}
		if codecs, err = a.Engine.StreamCodecs(ctx); err != nil {
			codecs = nil
			cam.Sources = append(cam.Sources, unavailable("go2rtc"))
		} else {
			cam.Sources = append(cam.Sources, Source{Name: "go2rtc", Status: SourceOK})
		}
	}
	if doc == nil {
		cam.Streams = inventoryStreams(in)
	} else {
		applyFrigateDoc(&cam, in, doc, codecs)
	}
	if doc != nil {
		cam.Detection.AvailableLabels = ModelLabels(ctx, a.Engine)
	}
	detectionView(&cam.Detection)
	finish(&cam, in, doc)
	deriveModules(&cam, in, doc)
	enrichVideo(ctx, &cam, a.Video)
	return cam, nil
}

// unavailable marks a source as unreadable. The cause is not copied: engine errors can quote
// URLs with credentials.
func unavailable(name string) Source {
	return Source{Name: name, Status: SourceUnavailable, Detail: "engine data could not be read"}
}

func baseModel(in Inventory) Camera {
	cam := Camera{
		Identity: Identity{
			CameraID: in.CameraID, Name: in.Name, RemoteName: in.RemoteName,
			Description: in.Description, Location: in.Location, Enabled: in.Enabled,
			Tags: append([]string{}, in.Tags...),
		},
		Origin:       Origin{ServerID: in.ServerID, ServerName: in.ServerName},
		Overrides:    Overrides{DefaultLiveQuality: in.DefaultLiveQuality},
		Zones:        []Zone{},
		Integrations: []Integration{},
		Detection:    Detection{Objects: []string{}},
		Sources:      []Source{},
		Warnings:     []string{},
	}
	if in.ONVIFEndpoint != "" {
		cam.Device.ONVIFVersion = in.ONVIFVersion
		cam.Device.ONVIFProfile = in.ONVIFProfile
		if u, err := url.Parse(in.ONVIFEndpoint); err == nil {
			cam.Device.Address = u.Hostname()
		}
	}
	return cam
}

// onvifCaps is the subset of the stored onvif_capabilities document the model reads.
type onvifCaps struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware_version"`
	Serial       string `json:"serial_number"`
	HasPTZ       bool   `json:"has_ptz"`
	HasAnalytics bool   `json:"has_analytics"`
	HasEvents    bool   `json:"has_events"`
}

func readONVIFCaps(raw []byte) (onvifCaps, bool) {
	var c onvifCaps
	if len(raw) == 0 || string(raw) == "{}" || json.Unmarshal(raw, &c) != nil {
		return onvifCaps{}, false
	}
	return c, true
}

func inventoryStreams(in Inventory) Streams {
	var s Streams
	if in.HQStream != "" {
		s.Primary = &Stream{Name: in.HQStream, Codec: in.HQCodec, Roles: []string{}}
	}
	if in.LiveStream != "" && in.LiveStream != in.HQStream {
		s.Detection = &Stream{Name: in.LiveStream, Codec: in.LiveCodec, Roles: []string{}}
	}
	return s
}

type ffmpegInput struct {
	name  string
	url   string
	roles []string
}

func (i ffmpegInput) has(role string) bool { return slices.Contains(i.roles, role) }

func readInputs(doc map[string]any) []ffmpegInput {
	ff, _ := doc["ffmpeg"].(map[string]any)
	list, _ := ff["inputs"].([]any)
	var out []ffmpegInput
	for _, it := range list {
		m, _ := it.(map[string]any)
		path, _ := m["path"].(string)
		in := ffmpegInput{url: RedactURL(path), name: streamName(path), roles: []string{}}
		for _, r := range anySlice(m["roles"]) {
			if s, ok := r.(string); ok {
				in.roles = append(in.roles, s)
			}
		}
		out = append(out, in)
	}
	return out
}

// streamName is the last path segment of an input URL, which for go2rtc restreams is the
// go2rtc stream name.
func streamName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Path[strings.LastIndex(u.Path, "/")+1:]
}

// pickPrimary chooses the main stream: the one go2rtc names as the camera's HQ stream, else
// the recording input, else the first one.
func pickPrimary(inputs []ffmpegInput, hq string) int {
	if hq != "" {
		for i, in := range inputs {
			if in.name == hq {
				return i
			}
		}
	}
	for i, in := range inputs {
		if in.has("record") {
			return i
		}
	}
	return 0
}

func indexWithRole(inputs []ffmpegInput, role string) int {
	for i, in := range inputs {
		if in.has(role) {
			return i
		}
	}
	return -1
}

func applyFrigateDoc(cam *Camera, in Inventory, doc map[string]any, codecs map[string]string) {
	inputs := readInputs(doc)
	detect, _ := doc["detect"].(map[string]any)

	codecFor := func(in ffmpegInput) string {
		if c := codecs[in.name]; c != "" && in.name != "" {
			return c
		}
		return ""
	}
	mk := func(i ffmpegInput) *Stream {
		return &Stream{Name: i.name, URL: i.url, Codec: codecFor(i), Roles: i.roles}
	}
	if len(inputs) > 0 {
		p := pickPrimary(inputs, in.HQStream)
		cam.Streams.Primary = mk(inputs[p])
		if cam.Streams.Primary.Codec == "" && inputs[p].name == in.HQStream {
			cam.Streams.Primary.Codec = in.HQCodec
		}
		if d := indexWithRole(inputs, "detect"); d >= 0 {
			cam.Streams.Detection = mk(inputs[d])
			if cam.Streams.Detection.Codec == "" && inputs[d].name == in.LiveStream {
				cam.Streams.Detection.Codec = in.LiveCodec
			}
			// Frigate's detect size and rate describe the detect input only.
			cam.Streams.Detection.Width = intOf(detect["width"])
			cam.Streams.Detection.Height = intOf(detect["height"])
			cam.Streams.Detection.FPS = floatOf(detect["fps"])
		}
		if r := indexWithRole(inputs, "record"); r >= 0 && r != p {
			cam.Streams.Recording = mk(inputs[r])
		}
	} else {
		cam.Streams = inventoryStreams(in)
	}

	cam.Recording.Enabled = boolPtr(doc, "record", "enabled")
	cam.Detection.Enabled = boolPtr(doc, "detect", "enabled")
	if objs, ok := doc["objects"].(map[string]any); ok {
		for _, o := range anySlice(objs["track"]) {
			if s, ok := o.(string); ok {
				cam.Detection.Objects = append(cam.Detection.Objects, s)
			}
		}
	}
	cam.Analytics.LPR = boolPtr(doc, "lpr", "enabled")
	cam.Analytics.Audio = boolPtr(doc, "audio", "enabled")
	if z, ok := doc["zones"].(map[string]any); ok {
		names := make([]string, 0, len(z))
		for n := range z {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			zone := Zone{Name: n, Objects: []string{}}
			if zm, ok := z[n].(map[string]any); ok {
				for _, o := range anySlice(zm["objects"]) {
					if s, ok := o.(string); ok {
						zone.Objects = append(zone.Objects, s)
					}
				}
			}
			cam.Zones = append(cam.Zones, zone)
		}
	}
	if on, ok := dig(doc, "onvif", "autotracking", "enabled").(bool); ok {
		cam.PTZ.Autotracking = &on
	}
}

// finish derives device data and capabilities. Every capability needs positive evidence.
func finish(cam *Camera, in Inventory, doc map[string]any) {
	caps, haveCaps := readONVIFCaps(in.ONVIFCapabilities)
	if haveCaps {
		cam.Device.Manufacturer, cam.Device.Model = caps.Manufacturer, caps.Model
		cam.Device.Firmware, cam.Device.SerialNumber = caps.Firmware, caps.Serial
	}
	cam.PTZ.Supported = haveCaps && caps.HasPTZ
	cam.Fisheye.Supported = hasAnyTag(in.Tags, "fisheye", "ojo_de_pez")
	lpr := in.LPR || hasAnyTag(in.Tags, "lpr_frigate", "lpr_provision") || isTrue(cam.Analytics.LPR)
	audio := isTrue(cam.Analytics.Audio)
	if cam.Streams.Primary != nil {
		audio = audio || slices.Contains(cam.Streams.Primary.Roles, "audio")
	}
	if doc != nil {
		for _, i := range readInputs(doc) {
			audio = audio || i.has("audio")
		}
	}

	var c []string
	add := func(ok bool, name string) {
		if ok {
			c = append(c, name)
		}
	}
	add(in.ONVIFEndpoint != "", CapONVIF)
	add(audio, CapAudio)
	add(cam.PTZ.Supported, CapPTZ)
	add(lpr, CapLPR)
	add(cam.Fisheye.Supported, CapFisheye)
	add(isTrue(cam.Detection.Enabled), CapObjectDetection)
	add(isTrue(cam.Recording.Enabled), CapRecording)
	add(haveCaps && caps.HasAnalytics, CapAnalytics)
	add(haveCaps && caps.HasEvents, CapEvents)
	if c == nil {
		c = []string{}
	}
	cam.Capabilities = c

	cam.Integrations = append(cam.Integrations, Integration{Name: "lpr", Enabled: lpr})
	if lpr && cam.Analytics.LPR == nil {
		on := true
		cam.Analytics.LPR = &on
	}
}

func hasAnyTag(tags []string, want ...string) bool {
	for _, t := range tags {
		if slices.Contains(want, strings.ToLower(strings.TrimSpace(t))) {
			return true
		}
	}
	return false
}

func isTrue(b *bool) bool { return b != nil && *b }

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func boolPtr(m map[string]any, keys ...string) *bool {
	if b, ok := dig(m, keys...).(bool); ok {
		return &b
	}
	return nil
}

func floatOf(v any) float64 {
	f, _ := v.(float64)
	return f
}

func intOf(v any) int { return int(floatOf(v)) }

// RedactURL reduces a stream URL to scheme, host and a credential-free path: userinfo (even
// when the password holds '@' or '/'), query, fragment and any credential-looking path segment
// onwards are dropped. Anything that does not look like an absolute URL yields "".
func RedactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i <= 0 || strings.ContainsAny(raw[:i], " \t/") {
		return ""
	}
	if out := safeURLToken(raw); out != hidden {
		return out
	}
	return ""
}
