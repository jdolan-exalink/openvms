package frigate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// v017 implements Adapter for Frigate 0.14 through 0.17 (review-item API).
type v017 struct {
	c       *client
	version string
}

func (a *v017) Name() string    { return "v017" }
func (a *v017) Version() string { return a.version }

type toggle struct {
	Enabled bool `json:"enabled"`
}

type cameraConfig struct {
	Enabled *bool          `json:"enabled"`
	Zones   map[string]any `json:"zones"`
	LPR     *toggle        `json:"lpr"`
	Audio   *toggle        `json:"audio"`
	ONVIF   *struct {
		Host string `json:"host"`
	} `json:"onvif"`
	Live *struct {
		Streams map[string]string `json:"streams"`
	} `json:"live"`
	Detect *toggle `json:"detect"`
	Objects *struct {
		Track []string `json:"track"`
	} `json:"objects"`
}

type frigateConfig struct {
	Cameras         map[string]cameraConfig `json:"cameras"`
	LPR             *toggle                 `json:"lpr"`
	Detect          *toggle                 `json:"detect"`
	Objects         *struct {
		Track []string `json:"track"`
	} `json:"objects"`
	FaceRecognition *toggle                 `json:"face_recognition"`
	SemanticSearch  *toggle                 `json:"semantic_search"`
}

func (a *v017) config(ctx context.Context) (frigateConfig, error) {
	var cfg frigateConfig
	err := a.c.getJSON(ctx, "/api/config", nil, &cfg)
	return cfg, err
}

func (a *v017) Capabilities(ctx context.Context) (Capabilities, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{
		Review:          true,
		Preview:         true,
		Exports:         true,
		LPR:             on(cfg.LPR),
		FaceRecognition: on(cfg.FaceRecognition),
		SemanticSearch:  on(cfg.SemanticSearch),
	}
	for _, cam := range cfg.Cameras {
		caps.LPR = caps.LPR || on(cam.LPR)
		caps.Audio = caps.Audio || on(cam.Audio)
		caps.PTZ = caps.PTZ || (cam.ONVIF != nil && cam.ONVIF.Host != "")
	}
	return caps, nil
}

func (a *v017) ListCameras(ctx context.Context) ([]Camera, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return nil, err
	}
	// go2rtc is what actually serves live streams; a name that config (or our own naming
	// guesses) offers but go2rtc doesn't have would only fail later with a generic
	// go2rtc error. Older Frigate builds (or go2rtc disabled) don't have this endpoint:
	// streams stays nil and pickStreams falls back to the pre-existing, unchecked
	// behaviour instead of breaking discovery.
	streams, err := a.go2rtcStreams(ctx)
	if err != nil {
		slog.DebugContext(ctx, "frigate: go2rtc streams unavailable, using legacy stream selection", "error", err)
		streams = nil
	}
	out := make([]Camera, 0, len(cfg.Cameras))
	for name, cc := range cfg.Cameras {
		cam := Camera{
			Name:    name,
			Enabled: cc.Enabled == nil || *cc.Enabled,
			LPR:     on(cc.LPR) || (cc.LPR == nil && on(cfg.LPR)),
			Zones:   make([]string, 0, len(cc.Zones)),
		}
		for z := range cc.Zones {
			cam.Zones = append(cam.Zones, z)
		}
		sort.Strings(cam.Zones)
		cam.LiveStream, cam.HQStream = pickStreams(name, cc, streams)
		out = append(out, cam)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// go2rtcStreams returns the set of stream names go2rtc actually serves, read from
// GET /api/go2rtc/streams. It returns an error (nil map) when the endpoint doesn't exist
// or go2rtc is disabled, which callers treat as "unknown" rather than "empty".
func (a *v017) go2rtcStreams(ctx context.Context) (map[string]bool, error) {
	var raw map[string]json.RawMessage
	if err := a.c.getJSON(ctx, "/api/go2rtc/streams", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(raw))
	for name := range raw {
		out[name] = true
	}
	return out, nil
}

// pickStreams chooses the grid substream and the high-quality stream for a camera.
//
// available is the set of stream names go2rtc actually serves (from go2rtcStreams). When
// it is nil (the endpoint failed or doesn't exist), stream selection falls back to the
// legacy, unchecked behaviour: trust live.streams, or the camera name when config
// declares none.
//
// When available is non-nil, a configured or guessed name is only used if go2rtc actually
// has it. Without a usable configured name, the camera name itself, then go2rtc naming
// conventions (<camera>_sub/_main/_live), are tried against go2rtc, case-insensitively. If
// nothing matches, the corresponding stream is left empty: this camera has no go2rtc
// restream, and the media gateway must not dial Frigate for it (see media.Gateway.live).
func pickStreams(name string, cc cameraConfig, available map[string]bool) (live, hq string) {
	live, hq = legacyPickStreams(name, cc)
	if available == nil {
		return live, hq
	}
	live = matchStream(live, available)
	hq = matchStream(hq, available)
	if live == "" {
		live = guessStream(name, available, "_sub")
	}
	if hq == "" {
		hq = guessStream(name, available, "_main")
	}
	return live, hq
}

// legacyPickStreams is the pre-go2rtc-aware selection: prefer live.streams labels that
// look like a sub/low stream for live and main/high/hq for hq, and fall back to the
// camera name (Frigate's go2rtc default) when config declares no streams at all.
func legacyPickStreams(name string, cc cameraConfig) (live, hq string) {
	if cc.Live == nil || len(cc.Live.Streams) == 0 {
		return name, name
	}
	labels := make([]string, 0, len(cc.Live.Streams))
	for l := range cc.Live.Streams {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	hq = cc.Live.Streams[labels[0]]
	live = hq
	for _, l := range labels {
		low := strings.ToLower(l + " " + cc.Live.Streams[l])
		if strings.Contains(low, "sub") || strings.Contains(low, "low") {
			live = cc.Live.Streams[l]
		} else if strings.Contains(low, "main") || strings.Contains(low, "high") || strings.Contains(low, "hq") {
			hq = cc.Live.Streams[l]
		}
	}
	return live, hq
}

// guessStream resolves a camera name against go2rtc's naming conventions: the exact
// camera name first (case-insensitive), then "<camera><suffix>" (e.g. "_sub"/"_main"),
// then the single-stream "<camera>_live" convention. Returns "" if nothing matches.
func guessStream(name string, available map[string]bool, suffix string) string {
	if s := matchStream(name, available); s != "" {
		return s
	}
	if s := matchStream(name+suffix, available); s != "" {
		return s
	}
	return matchStream(name+"_live", available)
}

// matchStream returns candidate's actual key in available (exact match first, then
// case-insensitive), or "" if go2rtc has no such stream.
func matchStream(candidate string, available map[string]bool) string {
	if candidate == "" || available == nil {
		return ""
	}
	if available[candidate] {
		return candidate
	}
	low := strings.ToLower(candidate)
	for s := range available {
		if strings.ToLower(s) == low {
			return s
		}
	}
	return ""
}

func (a *v017) Reviews(ctx context.Context, q ReviewQuery) ([]Review, error) {
	params := url.Values{}
	if len(q.Cameras) > 0 {
		params.Set("cameras", strings.Join(q.Cameras, ","))
	}
	if q.After > 0 {
		params.Set("after", strconv.FormatFloat(q.After, 'f', 6, 64))
	}
	if q.Before > 0 {
		params.Set("before", strconv.FormatFloat(q.Before, 'f', 6, 64))
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	var out []Review
	err := a.c.getJSON(ctx, "/api/review", params, &out)
	return out, err
}

type statsResponse struct {
	Cameras map[string]struct {
		CameraFPS    float64 `json:"camera_fps"`
		DetectionFPS float64 `json:"detection_fps"`
	} `json:"cameras"`
	Service struct {
		Uptime  int64  `json:"uptime"`
		Version string `json:"version"`
		Storage map[string]struct {
			Total float64 `json:"total"`
			Used  float64 `json:"used"`
			Free  float64 `json:"free"`
		} `json:"storage"`
	} `json:"service"`
}

func (a *v017) Stats(ctx context.Context) (Stats, error) {
	var r statsResponse
	if err := a.c.getJSON(ctx, "/api/stats", nil, &r); err != nil {
		return Stats{}, err
	}
	s := Stats{Version: r.Service.Version, UptimeSeconds: r.Service.Uptime, Cameras: map[string]CameraStats{}}
	for name, c := range r.Cameras {
		s.Cameras[name] = CameraStats{CameraFPS: c.CameraFPS, DetectionFPS: c.DetectionFPS}
	}
	if st, ok := r.Service.Storage["/media/frigate/recordings"]; ok {
		s.Recordings = &Storage{TotalMB: st.Total, UsedMB: st.Used, FreeMB: st.Free}
	}
	return s, nil
}

func on(t *toggle) bool { return t != nil && t.Enabled }

func (a *v017) GetCameraConfig(ctx context.Context, camera string) (CameraFrigateConfig, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return CameraFrigateConfig{}, err
	}
	cc, ok := cfg.Cameras[camera]
	if !ok {
		return CameraFrigateConfig{}, fmt.Errorf("camera %q: %w", camera, ErrNotFound)
	}

	detectEnabled := true
	if cc.Detect != nil {
		detectEnabled = cc.Detect.Enabled
	} else if cfg.Detect != nil {
		detectEnabled = cfg.Detect.Enabled
	}

	lprEnabled := on(cc.LPR) || (cc.LPR == nil && on(cfg.LPR))

	var tracked []string
	if cc.Objects != nil && len(cc.Objects.Track) > 0 {
		tracked = append(tracked, cc.Objects.Track...)
	} else if cfg.Objects != nil && len(cfg.Objects.Track) > 0 {
		tracked = append(tracked, cfg.Objects.Track...)
	}
	if tracked == nil {
		tracked = []string{}
	}
	sort.Strings(tracked)

	zones := make([]string, 0, len(cc.Zones))
	for z := range cc.Zones {
		zones = append(zones, z)
	}
	sort.Strings(zones)

	return CameraFrigateConfig{
		DetectEnabled:  detectEnabled,
		TrackedObjects: tracked,
		LPREnabled:     lprEnabled,
		Zones:          zones,
	}, nil
}

func (a *v017) UpdateCameraConfig(ctx context.Context, camera string, update CameraFrigateConfigUpdate) (CameraFrigateConfig, error) {
	if _, err := a.GetCameraConfig(ctx, camera); err != nil {
		return CameraFrigateConfig{}, err
	}

	params := url.Values{}
	if update.DetectEnabled != nil {
		params.Set(fmt.Sprintf("cameras.%s.detect.enabled", camera), strconv.FormatBool(*update.DetectEnabled))
	}
	if update.LPREnabled != nil {
		params.Set(fmt.Sprintf("cameras.%s.lpr.enabled", camera), strconv.FormatBool(*update.LPREnabled))
	}
	if update.TrackedObjects != nil {
		params.Set(fmt.Sprintf("cameras.%s.objects.track", camera), strings.Join(update.TrackedObjects, ","))
	}

	if len(params) > 0 {
		if _, err := a.c.send(ctx, http.MethodPut, "/api/config/set", params, nil); err != nil {
			return CameraFrigateConfig{}, err
		}
	}

	return a.GetCameraConfig(ctx, camera)
}

func (a *v017) Restart(ctx context.Context) error {
	_, err := a.c.send(ctx, http.MethodPost, "/api/restart", nil, nil)
	return err
}

