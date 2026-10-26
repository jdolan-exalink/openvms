package camconfig

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Sync states of a camera's configuration: desired (what OpenVMS wants) against effective
// (what the engine reports).
const (
	SyncSynced       = "synced"       // Sincronizado
	SyncPending      = "pending"      // Cambios pendientes
	SyncDrift        = "drift"        // Diferencia detectada
	SyncError        = "error"        // Error
	SyncUnverifiable = "unverifiable" // No verificable
)

// ValidSyncState reports whether s is one of the known sync states.
func ValidSyncState(s string) bool {
	switch s {
	case SyncSynced, SyncPending, SyncDrift, SyncError, SyncUnverifiable:
		return true
	}
	return false
}

// Drift is one field whose desired value differs from the effective one.
type Drift struct {
	Path      string `json:"path"`
	Desired   any    `json:"desired"`
	Effective any    `json:"effective"`
}

// Stored is the persisted desired state of a camera.
type Stored struct {
	Desired         Patch
	DesiredRevision int64
	AppliedRevision int64
	SyncState       string
	LastError       string
}

// State is the desired-versus-effective summary served with the model.
type State struct {
	DesiredRevision int64   `json:"desired_revision"`
	AppliedRevision int64   `json:"applied_revision"`
	SyncState       string  `json:"sync_state"`
	LastError       string  `json:"last_error"`
	Drift           []Drift `json:"drift"`
}

// EngineReadable reports whether the engine answered when the model was read.
func EngineReadable(c Camera) bool {
	for _, s := range c.Sources {
		if s.Name == "frigate" {
			return s.Status == SourceOK
		}
	}
	return false
}

// Diff lists the fields set in desired whose effective value differs. A field the engine did
// not report counts as different: absence is not agreement.
func Diff(desired Patch, effective Camera) []Drift {
	out := []Drift{}
	d := desired.Normalized()
	boolDiff := func(path string, want *bool, got *bool) {
		if want == nil || (got != nil && *got == *want) {
			return
		}
		var eff any
		if got != nil {
			eff = *got
		}
		out = append(out, Drift{Path: path, Desired: *want, Effective: eff})
	}
	if d.Detection != nil {
		boolDiff("detection.enabled", d.Detection.Enabled, effective.Detection.Enabled)
		if o := d.Detection.Objects; o != nil && !sameSet(effective.Detection.Objects, *o) {
			out = append(out, Drift{Path: "detection.objects", Desired: *o, Effective: nonNil(effective.Detection.Objects)})
		}
	}
	if d.Recording != nil {
		boolDiff("recording.enabled", d.Recording.Enabled, effective.Recording.Enabled)
	}
	if d.Analytics != nil {
		boolDiff("analytics.lpr", d.Analytics.LPR, effective.Analytics.LPR)
	}
	return out
}

// ComputeState derives what to show for a camera from its stored state and a fresh read.
//
//   - engine not readable: unverifiable (the stored error text is kept);
//   - differences with an apply in flight: pending; with a recorded failure: error; otherwise
//     drift (somebody changed the camera outside OpenVMS);
//   - no differences: synced.
func ComputeState(st Stored, effective Camera) State {
	out := State{DesiredRevision: st.DesiredRevision, AppliedRevision: st.AppliedRevision, LastError: st.LastError, Drift: []Drift{}}
	if !EngineReadable(effective) {
		out.SyncState = SyncUnverifiable
		return out
	}
	out.Drift = Diff(st.Desired, effective)
	switch {
	case len(out.Drift) == 0:
		out.SyncState, out.LastError = SyncSynced, ""
	case st.SyncState == SyncPending:
		out.SyncState = SyncPending
	case st.SyncState == SyncError:
		out.SyncState = SyncError
	default:
		out.SyncState = SyncDrift
	}
	return out
}

// Outcome is how one apply ended.
type Outcome struct {
	SyncState string
	LastError string
	// Applied is true when the engine confirmed the change, so applied_revision may advance.
	Applied bool
	Result  string // "ok" or "error" or "unverified", for the audit trail
}

// Result values recorded in the audit trail.
const (
	ResultOK         = "ok"
	ResultError      = "error"
	ResultUnverified = "unverified"
)

// DecideOutcome turns the engine call and the verification read into the final state. A failed
// apply is never synced, and only a verified change advances the applied revision.
func DecideOutcome(applyErr error, verified Camera, patch, desired Patch) Outcome {
	if applyErr != nil {
		return Outcome{SyncState: SyncError, LastError: SanitizeError(applyErr), Result: ResultError}
	}
	if !EngineReadable(verified) {
		return Outcome{SyncState: SyncUnverifiable, Result: ResultUnverified}
	}
	if len(Diff(patch, verified)) > 0 {
		return Outcome{SyncState: SyncError, LastError: "el motor no refleja el cambio aplicado", Result: ResultError}
	}
	if len(Diff(desired, verified)) > 0 {
		return Outcome{SyncState: SyncDrift, Applied: true, Result: ResultOK}
	}
	return Outcome{SyncState: SyncSynced, Applied: true, Result: ResultOK}
}

var (
	reUserinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]*@`)
	reQuery    = regexp.MustCompile(`(\?[^\s"']*)`)
	reSecret   = regexp.MustCompile(`(?i)(pass(word)?|pwd|token|secret|key|authorization)(["']?\s*[:=]\s*)\S+`)
)

const maxErrorLen = 300

// SanitizeError reduces an engine error to text that is safe to store and show: credentials in
// URLs, query strings and key=value secrets are removed and the length is capped.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	var ve *ValidationError
	msg := err.Error()
	if errors.As(err, &ve) {
		msg = ve.Msg
	}
	msg = reUserinfo.ReplaceAllString(msg, "${1}")
	msg = reQuery.ReplaceAllString(msg, "")
	msg = reSecret.ReplaceAllString(msg, "${1}${3}[oculto]")
	msg = strings.Join(strings.Fields(msg), " ")
	if utf8.RuneCountInString(msg) > maxErrorLen {
		msg = string([]rune(msg)[:maxErrorLen]) + "…"
	}
	if msg == "" {
		msg = fmt.Sprintf("%T", err)
	}
	return msg
}
