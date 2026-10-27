package camconfig

import (
	"encoding/json"
	"strings"
)

// Provenance sources: where the effective desired value of a field comes from.
const (
	// ProvEngine: nobody in OpenVMS set it; the value is what the engine reports (level 1).
	ProvEngine = "engine"
	// ProvProfile: defined by the camera's profile (level 2).
	ProvProfile = "profile"
	// ProvOverride: set on the camera itself (level 3); wins over the profile.
	ProvOverride = "override"
)

// ProfileRef identifies the profile a camera follows.
type ProfileRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	// Outdated is true when the profile changed after it was last applied to the camera.
	Outdated bool `json:"outdated"`
}

// Provenance is the origin of one field of the model.
type Provenance struct {
	Source      string `json:"source"`
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
}

// ValidateProfilePatch checks that a profile can hold the patch: the strict patch rules, plus
// no catalog selection (it is resolved per camera model, so a profile keeps raw object labels).
func ValidateProfilePatch(p Patch) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Detection != nil && p.Detection.Selection != nil {
		return invalid("profiles keep raw object labels in detection.objects; detection.selection is not allowed")
	}
	return nil
}

// leafKeys are objects treated as one value: their members only make sense together.
var leafKeys = map[string]bool{"resolution": true}

// Leaves flattens a patch to its leaf paths (dotted) and values.
func Leaves(p Patch) map[string]any {
	out := map[string]any{}
	raw, err := json.Marshal(p.Normalized())
	if err != nil {
		return out
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return out
	}
	var walk func(prefix string, v map[string]any)
	walk = func(prefix string, v map[string]any) {
		for k, x := range v {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if sub, ok := x.(map[string]any); ok && !leafKeys[k] {
				walk(path, sub)
				continue
			}
			out[path] = x
		}
	}
	walk("", m)
	return out
}

func fromLeaves(leaves map[string]any) Patch {
	root := map[string]any{}
	for path, v := range leaves {
		parts := strings.Split(path, ".")
		cur := root
		for _, part := range parts[:len(parts)-1] {
			next, _ := cur[part].(map[string]any)
			if next == nil {
				next = map[string]any{}
				cur[part] = next
			}
			cur = next
		}
		cur[parts[len(parts)-1]] = v
	}
	raw, _ := json.Marshal(root)
	var p Patch
	_ = json.Unmarshal(raw, &p)
	return p.Normalized()
}

// Subtract returns p without the fields that over also sets: what a profile still contributes
// to a camera whose own overrides win.
func Subtract(p, over Patch) Patch {
	l := Leaves(p)
	for path := range Leaves(over) {
		delete(l, path)
	}
	return fromLeaves(l)
}

// ResolveProvenance tells, for every field with a known value, whether it is inherited from
// the engine, defined by the profile or overridden on the camera. Engine entries are only
// reported for values the model actually holds: nothing is invented.
func ResolveProvenance(m Camera, overrides Patch, ref *ProfileRef, profile Patch) map[string]Provenance {
	out := map[string]Provenance{}
	if ref != nil {
		for path := range Leaves(profile) {
			out[path] = Provenance{Source: ProvProfile, ProfileID: ref.ID, ProfileName: ref.Name}
		}
	}
	for path := range Leaves(overrides) {
		out[path] = Provenance{Source: ProvOverride}
	}
	engine := func(path string, known bool) {
		if _, set := out[path]; !set && known {
			out[path] = Provenance{Source: ProvEngine}
		}
	}
	if EngineReadable(m) {
		engine("detection.enabled", m.Detection.Enabled != nil)
		engine("detection.objects", m.Detection.Objects != nil)
		engine("recording.enabled", m.Recording.Enabled != nil)
		engine("analytics.lpr", m.Analytics.LPR != nil)
	}
	for role, st := range map[string]*Stream{"primary": m.Streams.Primary, "detection": m.Streams.Detection} {
		if st == nil {
			continue
		}
		base := "streams." + role + "."
		engine(base+"resolution", st.Width > 0 && st.Height > 0)
		engine(base+"fps", st.FPS > 0)
		engine(base+"codec", st.Codec != "")
		engine(base+"bitrate_kbps", st.BitrateKbps > 0)
		engine(base+"gop", st.GOP > 0)
	}
	return out
}
