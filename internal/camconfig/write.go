package camconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// Origins say where a configuration change came from; they are stored in the audit trail.
const (
	OriginUI             = "ui"
	OriginProfile        = "profile"
	OriginGroup          = "group"
	OriginAPI            = "api"
	OriginSync           = "sync"
	OriginYAML           = "yaml"
	OriginExternalDevice = "external_device"
)

// ValidOrigin reports whether o is one of the known origins.
func ValidOrigin(o string) bool {
	switch o {
	case OriginUI, OriginProfile, OriginGroup, OriginAPI, OriginSync, OriginYAML, OriginExternalDevice:
		return true
	}
	return false
}

// ValidationError is a patch the model cannot represent or translate. It is a client error:
// nothing is ever silently dropped.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Patch is a sparse change to the normalized model: nil fields mean "leave as is".
//
// It is limited, on purpose, to the fields the model already has AND the Frigate adapter can
// translate today:
//
//	detection.enabled   -> detect.enabled
//	detection.objects   -> objects.track (raw engine labels until the object catalog lands)
//	recording.enabled   -> record.enabled
//	analytics.lpr       -> lpr.enabled
//	streams.primary|detection -> ONVIF encoder, or Frigate detect.width/height/fps (see VideoPatch)
//
// Anything else (zones, PTZ, recording mode...) is rejected by ParsePatch.
type Patch struct {
	Detection *DetectionPatch `json:"detection,omitempty"`
	Recording *RecordingPatch `json:"recording,omitempty"`
	Analytics *AnalyticsPatch `json:"analytics,omitempty"`
	// Streams changes the primary and detection video settings (see patch_video.go).
	Streams *StreamsPatch `json:"streams,omitempty"`
}

// DetectionPatch changes the detection policy.
type DetectionPatch struct {
	Enabled *bool     `json:"enabled,omitempty"`
	Objects *[]string `json:"objects,omitempty"`
}

// RecordingPatch changes the recording policy.
type RecordingPatch struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// AnalyticsPatch changes the analytics modules.
type AnalyticsPatch struct {
	LPR *bool `json:"lpr,omitempty"`
}

var objectLabel = regexp.MustCompile(`^[a-z0-9_]+$`)

// ParsePatch decodes and validates a patch. Unknown fields, wrong types, empty patches and
// invalid values are all rejected with a *ValidationError.
func ParsePatch(raw []byte) (Patch, error) {
	var p Patch
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Patch{}, invalid("patch not accepted: %s", err)
	}
	if dec.More() {
		return Patch{}, invalid("patch not accepted: trailing data")
	}
	if err := p.Validate(); err != nil {
		return Patch{}, err
	}
	return p.Normalized(), nil
}

// Validate checks that the patch changes something and holds valid values.
func (p Patch) Validate() error {
	if p.Empty() {
		return invalid("patch is empty")
	}
	if err := p.Streams.validate(); err != nil {
		return err
	}
	if p.Detection != nil && p.Detection.Objects != nil {
		for _, o := range *p.Detection.Objects {
			if !objectLabel.MatchString(o) {
				return invalid("detection.objects: %q is not a valid object label", o)
			}
		}
	}
	return nil
}

// Empty reports whether the patch changes nothing.
func (p Patch) Empty() bool {
	return (p.Detection == nil || (p.Detection.Enabled == nil && p.Detection.Objects == nil)) &&
		(p.Recording == nil || p.Recording.Enabled == nil) &&
		(p.Analytics == nil || p.Analytics.LPR == nil) &&
		streamsEmpty(p.Streams)
}

// Normalized drops empty branches and duplicate object labels, keeping order.
func (p Patch) Normalized() Patch {
	var out Patch
	if d := p.Detection; d != nil && (d.Enabled != nil || d.Objects != nil) {
		out.Detection = &DetectionPatch{Enabled: d.Enabled}
		if d.Objects != nil {
			objs := make([]string, 0, len(*d.Objects))
			for _, o := range *d.Objects {
				if !slices.Contains(objs, o) {
					objs = append(objs, o)
				}
			}
			out.Detection.Objects = &objs
		}
	}
	if r := p.Recording; r != nil && r.Enabled != nil {
		out.Recording = &RecordingPatch{Enabled: r.Enabled}
	}
	if a := p.Analytics; a != nil && a.LPR != nil {
		out.Analytics = &AnalyticsPatch{LPR: a.LPR}
	}
	out.Streams = p.Streams.normalized()
	return out
}

// Merge returns base overlaid with over: fields set in over win. Used to accumulate the
// desired state as a sparse patch.
func Merge(base, over Patch) Patch {
	out := base.Normalized()
	o := over.Normalized()
	if o.Detection != nil {
		if out.Detection == nil {
			out.Detection = &DetectionPatch{}
		}
		if o.Detection.Enabled != nil {
			out.Detection.Enabled = o.Detection.Enabled
		}
		if o.Detection.Objects != nil {
			out.Detection.Objects = o.Detection.Objects
		}
	}
	if o.Recording != nil {
		out.Recording = o.Recording
	}
	if o.Analytics != nil {
		if out.Analytics == nil {
			out.Analytics = &AnalyticsPatch{}
		}
		out.Analytics.LPR = o.Analytics.LPR
	}
	out.Streams = mergeStreams(out.Streams, o.Streams)
	return out
}

// Change is one line of a review: a field moving from its current to its new value.
type Change struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// ImpactItem is a consequence of applying the change, in words for the person reviewing it.
type ImpactItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Review is what applying a patch would do. Producing it has no side effects.
type Review struct {
	Changes     []Change     `json:"changes"`
	Impact      []ImpactItem `json:"impact"`
	Unsupported []string     `json:"unsupported"`
}

// Impact codes.
const (
	ImpactEngineReload          = "engine_reload"
	ImpactEngineRestart         = "engine_restart"
	ImpactRecordingInterruption = "recording_interruption"
	ImpactStreamRestart         = "stream_restart"
)

// Messages shown in the review's "Impacto".
const (
	msgEngineReload  = "Frigate recargará configuración;"
	msgEngineRestart = "reinicio de Frigate requerido;"
	msgRecording     = "grabación puede interrumpirse brevemente."
)

// SectionWriter applies a Frigate section patch (top-level section -> value) to one camera.
// The inventory service implements it with the same service behind the frigate-config
// endpoint, so validation, revisions and audit of that path are reused as they are.
type SectionWriter interface {
	PatchSections(ctx context.Context, sections map[string]any) error
}

// Plan implements Adapter. It is pure: it compares the patch with the current model and
// lists changes and impact; fields the current model already has are not listed.
func (a FrigateAdapter) Plan(ctx context.Context, current Camera, patch Patch) (Review, error) {
	if err := patch.Validate(); err != nil {
		return Review{}, err
	}
	p := patch.Normalized()
	rev := Review{Changes: []Change{}, Impact: []ImpactItem{}, Unsupported: []string{}}
	var sections []string
	add := func(section string, c Change) {
		rev.Changes = append(rev.Changes, c)
		if !slices.Contains(sections, section) {
			sections = append(sections, section)
		}
	}
	boolChange := func(section, path, label string, cur *bool, want *bool) {
		if want == nil || (cur != nil && *cur == *want) {
			return
		}
		var from any
		if cur != nil {
			from = *cur
		}
		add(section, Change{Path: path, Label: label, From: from, To: *want})
	}
	if p.Detection != nil {
		boolChange("detect", "detection.enabled", "detección de objetos", current.Detection.Enabled, p.Detection.Enabled)
		if o := p.Detection.Objects; o != nil && !sameSet(current.Detection.Objects, *o) {
			add("objects", Change{Path: "detection.objects", Label: "objetos rastreados",
				From: nonNil(current.Detection.Objects), To: *o})
		}
	}
	if p.Recording != nil {
		boolChange("record", "recording.enabled", "grabación", current.Recording.Enabled, p.Recording.Enabled)
	}
	if p.Analytics != nil {
		boolChange("lpr", "analytics.lpr", "lectura de matrículas (LPR)", current.Analytics.LPR, p.Analytics.LPR)
	}
	vp := a.planVideo(ctx, current, p.Streams)
	rev.Changes = append(rev.Changes, vp.changes...)
	rev.Unsupported = append(rev.Unsupported, vp.unsupported...)
	if vp.detect {
		sections = append(sections, "detect")
	}
	slices.Sort(sections)
	sections = slices.Compact(sections)
	reload, restart := false, false
	for _, s := range sections {
		if frigate.LiveSection(s) {
			reload = true
		} else {
			restart = true
		}
	}
	if restart {
		rev.Impact = append(rev.Impact, ImpactItem{Code: ImpactEngineRestart, Message: msgEngineRestart})
	}
	if reload {
		rev.Impact = append(rev.Impact, ImpactItem{Code: ImpactEngineReload, Message: msgEngineReload})
	}
	if slices.Contains(sections, "record") {
		rev.Impact = append(rev.Impact, ImpactItem{Code: ImpactRecordingInterruption, Message: msgRecording})
	}
	rev.Impact = append(rev.Impact, vp.impact...)
	return rev, nil
}

// Apply implements Adapter: it translates the patch to Frigate sections and hands them to
// the writer. Nothing is written when the patch does not validate.
func (a FrigateAdapter) Apply(ctx context.Context, patch Patch) error {
	sections, err := FrigateSections(patch)
	if err != nil {
		return err
	}
	p := patch.Normalized()
	// Camera video first: if the device refuses, nothing else was written.
	if err := a.applyVideo(ctx, p.Streams); err != nil {
		return err
	}
	if d := a.detectSection(p.Streams); len(d) > 0 {
		cur, _ := sections["detect"].(map[string]any)
		if cur == nil {
			cur = map[string]any{}
		}
		for k, v := range d {
			cur[k] = v
		}
		sections["detect"] = cur
	}
	if len(sections) == 0 {
		return nil
	}
	if a.Writer == nil {
		return errors.New("camconfig: the engine cannot be written")
	}
	return a.Writer.PatchSections(ctx, sections)
}

// FrigateSections translates a patch to Frigate's section patch.
func FrigateSections(patch Patch) (map[string]any, error) {
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	p := patch.Normalized()
	out := map[string]any{}
	if p.Detection != nil {
		if p.Detection.Enabled != nil {
			out["detect"] = map[string]any{"enabled": *p.Detection.Enabled}
		}
		if p.Detection.Objects != nil {
			out["objects"] = map[string]any{"track": *p.Detection.Objects}
		}
	}
	if p.Recording != nil {
		out["record"] = map[string]any{"enabled": *p.Recording.Enabled}
	}
	if p.Analytics != nil {
		out["lpr"] = map[string]any{"enabled": *p.Analytics.LPR}
	}
	return out, nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
