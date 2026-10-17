package frigate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrConfigPermissionRequired means a caller attempted a managed-zone write without servers.config authorization.
var ErrConfigPermissionRequired = errors.New("servers.config permission is required for Frigate writes")

// ErrConfigReadBackMismatch means Frigate accepted a patch but did not retain the requested value.
var ErrConfigReadBackMismatch = errors.New("camera-zone read-back from Frigate did not match the requested value")

// ConfigValidationError is a rejected patch or a configuration Frigate refused (HTTP 400).
type ConfigValidationError struct{ Message string }

func (e *ConfigValidationError) Error() string { return e.Message }

// SectionResult reports how one top-level camera section was applied.
type SectionResult struct {
	Section string `json:"section"`
	// AppliedLive is true when Frigate published the change to its running processes.
	AppliedLive bool `json:"applied_live"`
	// RequiresRestart is true when the change is saved to config.yml but only takes
	// effect after Frigate restarts.
	RequiresRestart bool `json:"requires_restart"`
}

// liveSections are the camera sections Frigate 0.17 can push to running processes through
// an update_topic (CameraConfigUpdateEnum in frigate/config/camera/updater.py, verified
// against the v0.17.0 source). Everything else is saved and needs a restart.
var liveSections = map[string]bool{
	"enabled": true, "audio": true, "audio_transcription": true, "birdseye": true,
	"detect": true, "motion": true, "notifications": true, "objects": true, "record": true,
	"review": true, "semantic_search": true, "snapshots": true, "zones": true,
}

// LiveSection reports whether section can be applied without restarting Frigate.
func LiveSection(section string) bool { return liveSections[section] }

var safeName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// redactedMarkers are what Frigate substitutes for secrets in GET /api/config.
var redactedCredentials = regexp.MustCompile(`://\*+:\*+@`)

// IsRedacted reports whether s is a value Frigate masked: credentials inside a URL
// (rtsp://*:*@host), or a bare run of asterisks / "<redacted>".
func IsRedacted(s string) bool {
	t := strings.TrimSpace(s)
	if redactedCredentials.MatchString(t) {
		return true
	}
	if strings.EqualFold(t, "<redacted>") || strings.EqualFold(t, "redacted") {
		return true
	}
	return t != "" && strings.Trim(t, "*") == ""
}

// FindRedacted returns the dotted path of the first redacted string inside v, if any.
func FindRedacted(v any) (string, bool) {
	return findRedacted("", v)
}

func findRedacted(path string, v any) (string, bool) {
	switch t := v.(type) {
	case string:
		if IsRedacted(t) {
			return path, true
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, ok := findRedacted(joinPath(path, k), t[k]); ok {
				return p, true
			}
		}
	case []any:
		for i, e := range t {
			if p, ok := findRedacted(joinPath(path, strconv.Itoa(i)), e); ok {
				return p, true
			}
		}
	}
	return "", false
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

// TouchesSecrets reports whether a camera patch edits credentials: ffmpeg inputs (stream
// URLs) or the ONVIF user/password. Such edits need the secrets permission.
func TouchesSecrets(patch map[string]any) bool {
	if ff, ok := patch["ffmpeg"].(map[string]any); ok {
		if _, has := ff["inputs"]; has {
			return true
		}
	}
	if on, ok := patch["onvif"].(map[string]any); ok {
		if _, has := on["user"]; has {
			return true
		}
		if _, has := on["password"]; has {
			return true
		}
	}
	return false
}

// --- schema -----------------------------------------------------------------------------

const schemaTTL = 10 * time.Minute

type schemaEntry struct {
	raw json.RawMessage
	exp time.Time
}

var (
	schemaMu    sync.Mutex
	schemaCache = map[string]schemaEntry{}
)

// ConfigSchema returns Frigate's pydantic JSON schema (GET /api/config/schema.json), cached
// per server and version for a few minutes.
func (a *v017) ConfigSchema(ctx context.Context) (json.RawMessage, error) {
	key := a.c.base.String() + "|" + a.version
	schemaMu.Lock()
	if e, ok := schemaCache[key]; ok && time.Now().Before(e.exp) {
		schemaMu.Unlock()
		return e.raw, nil
	}
	schemaMu.Unlock()
	body, err := a.c.get(ctx, "/api/config/schema.json", nil)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("frigate config schema is not valid JSON")
	}
	schemaMu.Lock()
	schemaCache[key] = schemaEntry{raw: body, exp: time.Now().Add(schemaTTL)}
	schemaMu.Unlock()
	return body, nil
}

// TrimCameraSchema reduces a full Frigate schema to the CameraConfig definition plus the
// $defs it references. A schema without $defs.CameraConfig is returned unchanged.
func TrimCameraSchema(full json.RawMessage) (json.RawMessage, error) {
	var root map[string]any
	if err := json.Unmarshal(full, &root); err != nil {
		return nil, err
	}
	defs, _ := root["$defs"].(map[string]any)
	if defs == nil {
		return full, nil
	}
	if _, ok := defs["CameraConfig"]; !ok {
		return full, nil
	}
	keep := map[string]any{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if ref, ok := t["$ref"].(string); ok {
				if name, found := strings.CutPrefix(ref, "#/$defs/"); found {
					if _, seen := keep[name]; !seen {
						if d, ok := defs[name]; ok {
							keep[name] = d
							walk(d)
						}
					}
				}
			}
			for _, e := range t {
				walk(e)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	keep["CameraConfig"] = defs["CameraConfig"]
	walk(defs["CameraConfig"])
	out := map[string]any{"$ref": "#/$defs/CameraConfig", "$defs": keep}
	if v, ok := root["$schema"]; ok {
		out["$schema"] = v
	}
	return json.Marshal(out)
}

// --- raw config ---------------------------------------------------------------------------

// RawConfig returns config.yml as Frigate stores it, secrets included (Frigate admin only).
func (a *v017) RawConfig(ctx context.Context) (string, error) {
	body, err := a.c.get(ctx, "/api/config/raw", nil)
	if err != nil {
		return "", err
	}
	s := string(body)
	// Some builds answer with a JSON-encoded string.
	if strings.HasPrefix(strings.TrimSpace(s), `"`) {
		var decoded string
		if json.Unmarshal(body, &decoded) == nil {
			return decoded, nil
		}
	}
	return s, nil
}

// RawPaths returns GET /api/config/raw_paths: the unmasked ffmpeg input paths per camera.
func (a *v017) RawPaths(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := a.c.getJSON(ctx, "/api/config/raw_paths", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveRawConfig validates and writes a full config.yml. restart also restarts Frigate.
// A configuration Frigate rejects comes back as *ConfigValidationError.
func (a *v017) SaveRawConfig(ctx context.Context, yamlText string, restart bool) error {
	opt := "saveonly"
	if restart {
		opt = "restart"
	}
	_, err := a.c.sendRaw(ctx, http.MethodPost, "/api/config/save", url.Values{"save_option": {opt}}, "text/plain", []byte(yamlText))
	return asValidationError(err)
}

func asValidationError(err error) error {
	var se *StatusError
	if errors.As(err, &se) && (se.Status == http.StatusBadRequest || se.Status == http.StatusUnprocessableEntity) {
		return &ConfigValidationError{Message: frigateMessage(se.Body)}
	}
	return err
}

// frigateMessage extracts {"message": "..."} (or a pydantic "detail") from an error body.
func frigateMessage(body string) string {
	var m struct {
		Message string `json:"message"`
		Detail  any    `json:"detail"`
	}
	if json.Unmarshal([]byte(body), &m) == nil {
		if m.Message != "" {
			return m.Message
		}
		if m.Detail != nil {
			b, _ := json.Marshal(m.Detail)
			return string(b)
		}
	}
	return strings.TrimSpace(body)
}

// --- camera config ------------------------------------------------------------------------

// CameraConfig returns the effective camera section of GET /api/config as generic JSON.
// Secrets stay masked unless secrets is true, which merges the unmasked ffmpeg input paths
// (raw_paths) and the ONVIF credentials (config.yml) back in.
func (a *v017) CameraConfig(ctx context.Context, camera string, secrets bool) (map[string]any, error) {
	var full struct {
		Cameras map[string]map[string]any `json:"cameras"`
	}
	if err := a.c.getJSON(ctx, "/api/config", nil, &full); err != nil {
		return nil, err
	}
	cam, ok := full.Cameras[camera]
	if !ok {
		return nil, fmt.Errorf("camera %q: %w", camera, ErrNotFound)
	}
	if !secrets {
		return cam, nil
	}
	if err := a.mergeSecrets(ctx, camera, cam); err != nil {
		return nil, err
	}
	return cam, nil
}

func (a *v017) mergeSecrets(ctx context.Context, camera string, cam map[string]any) error {
	paths, err := a.RawPaths(ctx)
	if err != nil {
		return err
	}
	rawInputs := dig(paths, "cameras", camera, "ffmpeg", "inputs")
	if list, ok := rawInputs.([]any); ok {
		if ff, ok := cam["ffmpeg"].(map[string]any); ok {
			if inputs, ok := ff["inputs"].([]any); ok && len(inputs) == len(list) {
				for i := range inputs {
					in, ok1 := inputs[i].(map[string]any)
					raw, ok2 := list[i].(map[string]any)
					if ok1 && ok2 && raw["path"] != nil {
						in["path"] = raw["path"]
					}
				}
			}
		}
	}
	text, err := a.RawConfig(ctx)
	if err != nil {
		return err
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(text), &parsed); err != nil {
		return fmt.Errorf("parse frigate config.yml: %w", err)
	}
	if on, ok := cam["onvif"].(map[string]any); ok {
		for _, k := range []string{"user", "password"} {
			if v := dig(parsed, "cameras", camera, "onvif", k); v != nil {
				on[k] = v
			}
		}
	}
	return nil
}

func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

// ApplyCameraPatch merges patch (top-level section -> value) into one camera's config. One
// request per section so each gets its own update_topic: sections Frigate can hot-reload
// are applied live, the rest are saved and flagged as needing a restart. Results lists the
// sections applied before any failure.
func (a *v017) ApplyCameraPatch(ctx context.Context, camera string, patch map[string]any) ([]SectionResult, error) {
	if !safeName.MatchString(camera) {
		return nil, &ConfigValidationError{Message: fmt.Sprintf("invalid camera name %q", camera)}
	}
	if len(patch) == 0 {
		return nil, &ConfigValidationError{Message: "empty patch"}
	}
	if p, bad := FindRedacted(patch); bad {
		return nil, &ConfigValidationError{Message: fmt.Sprintf("%s holds a redacted value; redacted values are never written back", p)}
	}
	sections := make([]string, 0, len(patch))
	for s := range patch {
		if !safeName.MatchString(s) {
			return nil, &ConfigValidationError{Message: fmt.Sprintf("invalid section name %q", s)}
		}
		sections = append(sections, s)
	}
	sort.Strings(sections)
	if _, err := a.CameraConfig(ctx, camera, false); err != nil {
		return nil, err
	}
	results := make([]SectionResult, 0, len(sections))
	for _, s := range sections {
		res := SectionResult{Section: s}
		res.AppliedLive = liveSections[s]
		res.RequiresRestart = !res.AppliedLive
		if err := a.setViaBody(ctx, camera, s, patch[s], res.AppliedLive); err != nil {
			return results, asValidationError(err)
		}
		results = append(results, res)
	}
	return results, nil
}

// UpsertManagedCameraZone patches one VMS-owned zone and verifies the effective config through
// the structured config API. Frigate's config_data merge preserves other zones and camera settings.
func (a *v017) UpsertManagedCameraZone(ctx context.Context, camera, zoneName string, zone map[string]any, hasConfigPermission bool) error {
	if !hasConfigPermission {
		return ErrConfigPermissionRequired
	}
	if !safeName.MatchString(camera) {
		return &ConfigValidationError{Message: fmt.Sprintf("invalid camera name %q", camera)}
	}
	if !safeName.MatchString(zoneName) {
		return &ConfigValidationError{Message: fmt.Sprintf("invalid zone name %q", zoneName)}
	}
	if len(zone) == 0 {
		return &ConfigValidationError{Message: "managed zone config is empty"}
	}
	if p, bad := FindRedacted(zone); bad {
		return &ConfigValidationError{Message: fmt.Sprintf("%s holds a redacted value; redacted values are never written back", p)}
	}
	remoteName := zoneName
	if !strings.HasPrefix(remoteName, "ovms_") {
		remoteName = "ovms_" + remoteName
	}
	results, err := a.ApplyCameraPatch(ctx, camera, map[string]any{"zones": map[string]any{remoteName: zone}})
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0].Section != "zones" || !results[0].AppliedLive || results[0].RequiresRestart {
		return fmt.Errorf("managed camera-zone patch did not apply live")
	}
	cfg, err := a.CameraConfig(ctx, camera, false)
	if err != nil {
		return err
	}
	zones, ok := cfg["zones"].(map[string]any)
	if !ok || !reflect.DeepEqual(zones[remoteName], zone) {
		return ErrConfigReadBackMismatch
	}
	return nil
}

// DeleteManagedCameraZone removes one explicitly OpenVMS-owned zone and verifies absence.
// Callers must only invoke this for a persisted zone with a last-applied snapshot.
func (a *v017) DeleteManagedCameraZone(ctx context.Context, camera, zoneName string, hasConfigPermission bool) error {
	if !hasConfigPermission {
		return ErrConfigPermissionRequired
	}
	if !safeName.MatchString(camera) || !safeName.MatchString(zoneName) {
		return &ConfigValidationError{Message: "invalid camera or zone name"}
	}
	if !strings.HasPrefix(zoneName, "ovms_") {
		zoneName = "ovms_" + zoneName
	}
	results, err := a.ApplyCameraPatch(ctx, camera, map[string]any{"zones": map[string]any{zoneName: ""}})
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0].Section != "zones" || !results[0].AppliedLive || results[0].RequiresRestart {
		return fmt.Errorf("managed camera-zone deletion did not apply live")
	}
	cfg, err := a.CameraConfig(ctx, camera, false)
	if err != nil {
		return err
	}
	zones, _ := cfg["zones"].(map[string]any)
	if _, exists := zones[zoneName]; exists {
		return ErrConfigReadBackMismatch
	}
	return nil
}

func (a *v017) setViaBody(ctx context.Context, camera, section string, value any, live bool) error {
	body := map[string]any{
		"config_data":      map[string]any{"cameras": map[string]any{camera: map[string]any{section: value}}},
		"requires_restart": 1,
	}
	if live {
		body["requires_restart"] = 0
		body["update_topic"] = "config/cameras/" + camera + "/" + section
	}
	_, err := a.c.send(ctx, http.MethodPut, "/api/config/set", nil, body)
	return err
}
