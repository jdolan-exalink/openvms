package camconfig

import (
	"fmt"
	"sort"
	"strings"
)

// Rollback (PRD §32): "guardar estado anterior; aplicar; validar; si falla, ofrecer rollback".
// The previous state of every normalized change is already in the audit trail
// (CAMERA_CONFIG_CHANGED.previous_values), so a rollback is a new change built from those old
// values and sent through the normal review and apply pipeline.

// RollbackSource is what a rollback needs from one audit record.
type RollbackSource struct {
	Origin   string
	Result   string
	Previous map[string]any
}

// Rollbackable reports whether the recorded change can be undone through the Patch type, and
// otherwise why not (in words for the person).
func Rollbackable(src RollbackSource) (bool, string) {
	if src.Result == ResultError {
		return false, "el cambio no llegó a aplicarse"
	}
	if src.Origin == OriginYAML {
		return false, "los cambios hechos con YAML se restauran editando el YAML"
	}
	if _, err := InversePatch(src.Previous); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// InversePatch builds the patch that puts every changed field back to its recorded previous
// value. It refuses (a *ValidationError) when any changed field has no recorded old value or
// is not something the Patch type can write: a partial rollback would be a different change
// than the one the person asked for.
func InversePatch(previous map[string]any) (Patch, error) {
	var p Patch
	var problems []string
	paths := make([]string, 0, len(previous))
	for path := range previous {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		old := previous[path]
		switch {
		case strings.HasPrefix(path, "detection.selection."):
			// Derived from detection.objects, which is restored on its own.
			continue
		case strings.HasPrefix(path, "streams."):
			problems = append(problems, "los cambios de video no se pueden restaurar automáticamente ("+path+")")
		case path == "detection.enabled":
			b, ok := old.(bool)
			if !ok {
				problems = append(problems, noOld(path))
				continue
			}
			p.Detection = detection(p.Detection)
			p.Detection.Enabled = &b
		case path == "detection.objects":
			objs, ok := stringList(old)
			if !ok {
				problems = append(problems, noOld(path))
				continue
			}
			p.Detection = detection(p.Detection)
			p.Detection.Objects = &objs
		case path == "recording.enabled":
			b, ok := old.(bool)
			if !ok {
				problems = append(problems, noOld(path))
				continue
			}
			p.Recording = &RecordingPatch{Enabled: &b}
		case path == "analytics.lpr":
			b, ok := old.(bool)
			if !ok {
				problems = append(problems, noOld(path))
				continue
			}
			p.Analytics = analytics(p.Analytics)
			p.Analytics.LPR = &b
		case strings.HasPrefix(path, "analytics.modules."):
			if msg := inverseModule(&p, path, old); msg != "" {
				problems = append(problems, msg)
			}
		default:
			problems = append(problems, "no se puede restaurar "+path)
		}
	}
	if len(problems) > 0 {
		return Patch{}, invalid("no se puede restaurar la configuración anterior: %s", strings.Join(problems, "; "))
	}
	if p.Empty() {
		return Patch{}, invalid("no se puede restaurar la configuración anterior: el cambio no registró valores anteriores")
	}
	if err := p.Validate(); err != nil {
		return Patch{}, err
	}
	return p.Normalized(), nil
}

func noOld(path string) string { return "no se conoce el valor anterior de " + path }

func detection(d *DetectionPatch) *DetectionPatch {
	if d == nil {
		return &DetectionPatch{}
	}
	return d
}

func analytics(a *AnalyticsPatch) *AnalyticsPatch {
	if a == nil {
		return &AnalyticsPatch{}
	}
	return a
}

func stringList(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		return append([]string{}, t...), true
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// inverseModule handles analytics.modules.<id>.enabled and .settings.min_area.
func inverseModule(p *Patch, path string, old any) string {
	rest := strings.TrimPrefix(path, "analytics.modules.")
	id, field, ok := strings.Cut(rest, ".")
	if !ok || id == "" {
		return "no se puede restaurar " + path
	}
	p.Analytics = analytics(p.Analytics)
	if p.Analytics.Modules == nil {
		p.Analytics.Modules = map[string]*ModulePatch{}
	}
	mp := p.Analytics.Modules[id]
	if mp == nil {
		mp = &ModulePatch{}
		p.Analytics.Modules[id] = mp
	}
	switch field {
	case "enabled":
		b, ok := old.(bool)
		if !ok {
			return noOld(path)
		}
		mp.Enabled = &b
	case "settings.min_area":
		f, ok := old.(float64)
		if !ok {
			if i, isInt := old.(int); isInt {
				f, ok = float64(i), true
			}
		}
		if !ok || f != float64(int(f)) {
			return noOld(path)
		}
		n := int(f)
		mp.Settings = &ModuleSettingsPatch{MinArea: &n}
	default:
		return fmt.Sprintf("no se puede restaurar %s", path)
	}
	return ""
}
