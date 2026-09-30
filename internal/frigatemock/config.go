package frigatemock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetCall records one PUT /api/config/set the mock applied, so tests can assert what
// OpenVMS sent: dotted query parameters (Frigate 0.16 style) or a config_data body with
// requires_restart and update_topic (0.17 style).
type SetCall struct {
	Query           map[string]string
	RequiresRestart *int
	UpdateTopic     string
	ConfigData      map[string]any
}

// SaveCall records one POST /api/config/save.
type SaveCall struct {
	Option string
	Body   string
}

// SetCalls returns the config/set requests received so far.
func (s *Server) SetCalls() []SetCall {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return append([]SetCall(nil), s.setCalls...)
}

// SaveCalls returns the config/save requests received so far.
func (s *Server) SaveCalls() []SaveCall {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return append([]SaveCall(nil), s.saves...)
}

// ConfigYAML renders the current configuration like GET /api/config/raw does.
func (s *Server) ConfigYAML() string {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.renderLocked()
}

// liveTopics mirrors CameraConfigUpdateEnum of Frigate 0.17.
var liveTopics = map[string]bool{
	"add": true, "audio": true, "audio_transcription": true, "birdseye": true, "detect": true,
	"enabled": true, "motion": true, "notifications": true, "objects": true, "object_genai": true,
	"record": true, "remove": true, "review": true, "review_genai": true, "semantic_search": true,
	"snapshots": true, "zones": true,
}

var maskCreds = regexp.MustCompile(`://[^/@]*@`)

// versionAtLeast compares the mock's version ("0.17.2-mock") against major.minor.
func (s *Server) versionAtLeast(major, minor int) bool {
	parts := strings.SplitN(strings.SplitN(s.Version, "-", 2)[0], ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, _ := strconv.Atoi(parts[0])
	mi, _ := strconv.Atoi(parts[1])
	return ma > major || (ma == major && mi >= minor)
}

// stateLocked lazily builds the configuration from Cameras. Callers hold cfgMu.
func (s *Server) stateLocked() map[string]any {
	if s.cfg != nil {
		return s.cfg
	}
	cams := map[string]any{}
	for _, c := range s.Cameras {
		zones := map[string]any{}
		for _, z := range c.Zones {
			zones[z] = map[string]any{"coordinates": "0,0,1,0,1,1,0,1"}
		}
		detectEnabled := true
		if c.Detect != nil {
			detectEnabled = *c.Detect
		}
		tracked := c.TrackedObjects
		if tracked == nil {
			tracked = []string{"person"}
		}
		cams[c.Name] = map[string]any{
			"name":    c.Name,
			"enabled": true,
			"detect":  map[string]any{"enabled": detectEnabled, "width": 1280, "height": 720, "fps": 5},
			"record":  map[string]any{"enabled": true, "retain": map[string]any{"days": 7}},
			"live":    map[string]any{"streams": map[string]string{"main": c.Name, "sub": c.Name + "_sub"}},
			"lpr":     map[string]any{"enabled": c.LPR},
			"objects": map[string]any{"track": tracked},
			"zones":   zones,
			"ffmpeg": map[string]any{
				"inputs":       []any{map[string]any{"path": "rtsp://viewer:s3cret@10.0.0.10:554/" + c.Name, "roles": []any{"detect", "record"}}},
				"hwaccel_args": "preset-vaapi",
			},
			"onvif": map[string]any{"host": "10.0.0.10", "port": 8000, "user": "onvifuser", "password": "onvifpass"},
		}
	}
	s.cfg = normalize(map[string]any{
		"cameras": cams,
		"mqtt":    map[string]any{"enabled": true, "topic_prefix": s.TopicPrefix},
		"version": s.Version,
		"go2rtc":  map[string]any{"streams": map[string]any{}},
	})
	return s.cfg
}

// normalize round-trips through JSON so every value is a plain map/slice/float64/string.
func normalize(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				deepMerge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func (s *Server) renderLocked() string {
	out, _ := yaml.Marshal(s.stateLocked())
	return string(out)
}

// masked returns a copy of the config with credentials masked the way Frigate does.
func masked(cfg map[string]any) map[string]any {
	out := normalize(cfg)
	cams, _ := out["cameras"].(map[string]any)
	for _, c := range cams {
		cam, _ := c.(map[string]any)
		if ff, ok := cam["ffmpeg"].(map[string]any); ok {
			if inputs, ok := ff["inputs"].([]any); ok {
				for _, in := range inputs {
					if m, ok := in.(map[string]any); ok {
						if p, ok := m["path"].(string); ok {
							m["path"] = maskCreds.ReplaceAllString(p, "://*:*@")
						}
					}
				}
			}
		}
		if on, ok := cam["onvif"].(map[string]any); ok {
			for _, k := range []string{"user", "password"} {
				if _, has := on[k]; has {
					on[k] = "*"
				}
			}
		}
	}
	return out
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	s.cfgMu.Lock()
	out := masked(s.stateLocked())
	s.cfgMu.Unlock()
	writeJSON(w, out)
}

func (s *Server) configRaw(w http.ResponseWriter, _ *http.Request) {
	s.cfgMu.Lock()
	text := s.renderLocked()
	s.cfgMu.Unlock()
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(text))
}

func (s *Server) configRawPaths(w http.ResponseWriter, _ *http.Request) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	cams := map[string]any{}
	cfgCams, _ := s.stateLocked()["cameras"].(map[string]any)
	for name, c := range cfgCams {
		cam, _ := c.(map[string]any)
		if ff, ok := cam["ffmpeg"].(map[string]any); ok {
			cams[name] = map[string]any{"ffmpeg": map[string]any{"inputs": ff["inputs"]}}
		}
	}
	writeJSON(w, map[string]any{"cameras": cams, "go2rtc": map[string]any{"streams": map[string]any{}}})
}

func (s *Server) configSchema(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, configSchema())
}

// validate applies the handful of checks the mock enforces, returning Frigate's 400 text.
func validate(cfg map[string]any) error {
	cams, ok := cfg["cameras"].(map[string]any)
	if !ok {
		return fmt.Errorf("cameras: Field required")
	}
	names := make([]string, 0, len(cams))
	for n := range cams {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		cam, ok := cams[n].(map[string]any)
		if !ok {
			return fmt.Errorf("cameras.%s: Input should be a valid dictionary", n)
		}
		ff, _ := cam["ffmpeg"].(map[string]any)
		inputs, _ := ff["inputs"].([]any)
		if len(inputs) == 0 {
			return fmt.Errorf("cameras.%s.ffmpeg.inputs: List should have at least 1 item", n)
		}
		for i, in := range inputs {
			m, _ := in.(map[string]any)
			if p, _ := m["path"].(string); p == "" {
				return fmt.Errorf("cameras.%s.ffmpeg.inputs.%d.path: Field required", n, i)
			}
		}
		if det, ok := cam["detect"].(map[string]any); ok {
			if fps, ok := det["fps"]; ok {
				if f, isNum := fps.(float64); !isNum || f < 1 {
					return fmt.Errorf("cameras.%s.detect.fps: Input should be a valid integer greater than or equal to 1", n)
				}
			}
		}
	}
	return nil
}

func invalidConfig(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": "Your configuration is invalid.\nSee the official documentation to learn how to edit your config.yml. Error: " + err.Error(),
	})
}

func (s *Server) configSave(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	var parsed map[string]any
	if err := yaml.Unmarshal(body, &parsed); err != nil || parsed == nil {
		if err == nil {
			err = fmt.Errorf("empty config")
		}
		invalidConfig(w, err)
		return
	}
	parsed = normalize(parsed)
	if err := validate(parsed); err != nil {
		invalidConfig(w, err)
		return
	}
	s.cfgMu.Lock()
	s.stateLocked()
	s.cfg = parsed
	s.saves = append(s.saves, SaveCall{Option: r.URL.Query().Get("save_option"), Body: string(body)})
	s.cfgMu.Unlock()
	writeJSON(w, map[string]any{"success": true, "message": "Config successfully saved."})
}

// setPath writes value at a dotted path, creating objects on the way.
func setPath(cfg map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cur := cfg
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func parseQueryValue(key, v string) any {
	switch {
	case v == "true" || v == "false":
		return v == "true"
	case v == "[]":
		return []any{}
	case strings.HasPrefix(v, "["):
		var items []any
		if json.Unmarshal([]byte(v), &items) == nil {
			return items
		}
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	if strings.Contains(v, ",") || strings.HasSuffix(key, ".track") {
		items := []any{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				items = append(items, p)
			}
		}
		return items
	}
	return v
}

func (s *Server) setConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequiresRestart *int           `json:"requires_restart"`
		UpdateTopic     string         `json:"update_topic"`
		ConfigData      map[string]any `json:"config_data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	call := SetCall{Query: map[string]string{}, RequiresRestart: body.RequiresRestart, UpdateTopic: body.UpdateTopic}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	next := normalize(s.stateLocked())
	for k, vals := range r.URL.Query() {
		if k == "" || len(vals) == 0 {
			continue
		}
		call.Query[k] = vals[0]
		setPath(next, k, parseQueryValue(k, vals[0]))
	}
	switch {
	case len(call.Query) > 0:
	case body.ConfigData != nil && s.versionAtLeast(0, 17):
		call.ConfigData = body.ConfigData
		deepMerge(next, normalize(body.ConfigData))
	default:
		http.Error(w, `{"message":"No configuration data provided"}`, http.StatusBadRequest)
		return
	}
	if err := validate(next); err != nil {
		invalidConfig(w, err)
		return
	}
	s.cfg = next
	s.setCalls = append(s.setCalls, call)
	if body.UpdateTopic != "" && s.versionAtLeast(0, 17) {
		parts := strings.Split(body.UpdateTopic, "/")
		if len(parts) != 4 || parts[0] != "config" || parts[1] != "cameras" || !liveTopics[parts[3]] {
			// Frigate writes the file first and fails publishing; mimic that.
			http.Error(w, `{"success":false,"message":"Error updating config"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"success": true, "message": "Config successfully updated, restart to apply"})
}
