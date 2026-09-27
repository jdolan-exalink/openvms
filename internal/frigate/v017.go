package frigate

import (
	"context"
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
}

type frigateConfig struct {
	Cameras         map[string]cameraConfig `json:"cameras"`
	LPR             *toggle                 `json:"lpr"`
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
		cam.LiveStream, cam.HQStream = pickStreams(name, cc)
		out = append(out, cam)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// pickStreams chooses the grid substream and the high-quality stream from live.streams.
// Without explicit streams Frigate serves the go2rtc stream named after the camera.
func pickStreams(name string, cc cameraConfig) (live, hq string) {
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
