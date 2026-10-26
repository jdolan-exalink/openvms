package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Contextual YAML editing of one camera section (PRD §34): read the YAML of a section,
// validate an edit (syntax, schema, diff, parts OpenVMS manages) and apply it through the same
// sectioned PATCH the schema editor uses. Nothing here talks to Frigate differently from
// PatchCameraFrigateConfig, which keeps its permission checks, rollback copy and revisions.

var yamlSectionName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// YAMLSection is one section of a camera's Frigate configuration as YAML.
type YAMLSection struct {
	CameraID uuid.UUID
	Section  string
	YAML     string
	// Exists is false when the camera has no such section yet (the YAML is then empty).
	Exists bool
	// SecretsVisible is true when the YAML carries unmasked credentials. Otherwise redacted
	// values show as placeholders that are safe to leave in place when saving.
	SecretsVisible bool
	// ManagedPaths are the parts of the section OpenVMS owns; paths are relative to the section.
	ManagedPaths []camconfig.ManagedPath
	// Revision is the camera's desired-state revision to send back as expected_revision.
	Revision int64
}

// YAMLValidation is the result of checking an edit without writing anything.
type YAMLValidation struct {
	Section string
	// Valid is true when there is no syntax or schema error. Managed conflicts do not make an
	// edit invalid; they must be confirmed when it is applied.
	Valid  bool
	Syntax []camconfig.SyntaxIssue
	Schema []camconfig.SchemaIssue
	// SchemaChecked is false when the engine's schema could not be read.
	SchemaChecked    bool
	Changes          []camconfig.LeafChange
	ManagedConflicts []camconfig.ManagedConflict
	Warnings         []string
	Revision         int64
}

// YAMLApplyInput is a request to save an edited section.
type YAMLApplyInput struct {
	Section                 string
	YAML                    string
	ExpectedRevision        int64
	ConfirmManagedConflicts bool
}

// YAMLApplyResult is the outcome of a saved edit.
type YAMLApplyResult struct {
	Validation YAMLValidation
	Patch      FrigatePatchResult
	// Marked is true when managed fields were changed and the camera was marked as drifted.
	Marked bool
	// Revision is the camera's desired-state revision after the edit.
	Revision int64
	// SyncState is the camera's stored sync state after the edit.
	SyncState string
}

// ManagedConflictError is an edit that changes parts OpenVMS manages without confirmation.
type ManagedConflictError struct{ Conflicts []camconfig.ManagedConflict }

func (e *ManagedConflictError) Error() string {
	paths := make([]string, 0, len(e.Conflicts))
	for _, c := range e.Conflicts {
		paths = append(paths, c.EnginePath)
	}
	return "the edit changes values managed by OpenVMS (" + strings.Join(paths, ", ") + "); confirm to apply it"
}

type yamlTarget struct {
	cam        db.GetCameraRow
	srv        db.GetServerRow
	canSecrets bool
	desired    camconfig.Patch
	revision   int64
}

// requireYAML is the permission to read and edit the YAML of a camera: today the raw-YAML
// rule, servers.config together with servers.config.secrets.
func requireYAML(c *access.Checker, _ db.GetCameraRow, srv db.GetServerRow) error {
	res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
	if err := c.Require(authz.ServersConfig, res); err != nil {
		return err
	}
	return c.Require(authz.ServersConfigSecrets, res)
}

func (s *Service) loadYAMLTarget(ctx context.Context, actor authz.Actor, id uuid.UUID) (yamlTarget, error) {
	var t yamlTarget
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if t.cam, err = q.GetCamera(ctx, id); err != nil {
			return notFoundOr(err)
		}
		if t.srv, err = q.GetServer(ctx, t.cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		if err := requireYAML(c, t.cam, t.srv); err != nil {
			return err
		}
		t.canSecrets = c.Can(authz.ServersConfigSecrets, access.Server(t.srv.TenantID, t.srv.SiteID, t.srv.ID))
		return loadDesired(ctx, q, &t)
	})
	return t, err
}

// loadDesired reads the camera's effective desired state (profile overlaid with overrides).
func loadDesired(ctx context.Context, q *db.Queries, t *yamlTarget) error {
	row, err := q.GetCameraConfigState(ctx, t.cam.ID)
	switch {
	case errors.Is(store.Classify(err), store.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	inherited, err := profilePatchOf(ctx, q, row.ProfileID)
	if err != nil {
		return err
	}
	t.desired = camconfig.Merge(inherited, StoredConfigState(&row).Desired)
	t.revision = row.DesiredRevision
	return nil
}

// yamlView is a camera section as the actor sees it and as it really is.
type yamlView struct {
	t       yamlTarget
	a       frigate.Adapter
	section string
	// shown is what the actor sees (credentials masked unless they may read them); actual is the
	// unmasked section when the engine can provide it, nil otherwise. exists tells whether the
	// camera has the section at all.
	shown, actual any
	exists        bool
	secrets       bool
	schema        json.RawMessage
	zones         any
}

func (s *Service) openYAML(ctx context.Context, actor authz.Actor, id uuid.UUID, section string) (yamlView, error) {
	if !yamlSectionName.MatchString(section) {
		return yamlView{}, invalid("section %q is not a camera section", section)
	}
	t, err := s.loadYAMLTarget(ctx, actor, id)
	if err != nil {
		return yamlView{}, err
	}
	a, err := s.frigateAdapter(ctx, t.srv)
	if err != nil {
		return yamlView{}, err
	}
	masked, err := a.CameraConfig(ctx, t.cam.RemoteName, false)
	if err != nil {
		return yamlView{}, mapCameraReadErr(err)
	}
	v := yamlView{t: t, a: a, section: section}
	full, ferr := a.CameraConfig(ctx, t.cam.RemoteName, true)
	if ferr != nil {
		slog.WarnContext(ctx, "camera yaml: secrets view unavailable", "camera_id", t.cam.ID)
		full = nil
	}
	shownDoc := masked
	if t.canSecrets && full != nil {
		shownDoc, v.secrets = full, true
	}
	if raw, err := a.ConfigSchema(ctx); err == nil {
		if trimmed, terr := frigate.TrimCameraSchema(raw); terr == nil {
			v.schema = trimmed
		}
	}
	if v.schema != nil {
		if !slices.Contains(camconfig.SectionNames(v.schema), section) {
			return yamlView{}, invalid("%q is not a section of the camera configuration", section)
		}
	} else if _, ok := masked[section]; !ok {
		return yamlView{}, invalid("%q is not a section of the camera configuration", section)
	}
	v.shown, v.exists = canonicalSection(shownDoc, section)
	if full != nil {
		v.actual, _ = canonicalSection(full, section)
	}
	v.zones = masked["zones"]
	return v, nil
}

func mapCameraReadErr(err error) error {
	if errors.Is(err, frigate.ErrNotFound) {
		return err
	}
	return &FrigateError{Err: err}
}

func canonicalSection(doc map[string]any, section string) (any, bool) {
	raw, ok := doc[section]
	if !ok {
		return nil, false
	}
	c, err := camconfig.Canonical(raw)
	if err != nil {
		return nil, false
	}
	return c, true
}

// managed lists what OpenVMS owns inside the section, relative to it.
func (v yamlView) managed() []camconfig.ManagedPath {
	all := append(camconfig.ManagedPaths(v.t.desired), camconfig.ManagedZonePaths(v.zones)...)
	return camconfig.InSection(all, v.section)
}

// GetCameraYAMLSection returns the YAML of one camera section with the parts OpenVMS manages.
func (s *Service) GetCameraYAMLSection(ctx context.Context, actor authz.Actor, id uuid.UUID, section string) (YAMLSection, error) {
	v, err := s.openYAML(ctx, actor, id, section)
	if err != nil {
		return YAMLSection{}, err
	}
	text, err := camconfig.SectionYAML(v.shown)
	if err != nil {
		return YAMLSection{}, err
	}
	managed := v.managed()
	if managed == nil {
		managed = []camconfig.ManagedPath{}
	}
	return YAMLSection{
		CameraID: v.t.cam.ID, Section: section, YAML: text, Exists: v.exists, SecretsVisible: v.secrets,
		ManagedPaths: managed, Revision: v.t.revision,
	}, nil
}

// checked is a validated edit: the parsed document and everything the response needs.
type checked struct {
	val    YAMLValidation
	edited any
}

func (v yamlView) check(text string) (checked, error) {
	out := checked{val: YAMLValidation{
		Section: v.section, Syntax: []camconfig.SyntaxIssue{}, Schema: []camconfig.SchemaIssue{},
		Changes: []camconfig.LeafChange{}, ManagedConflicts: []camconfig.ManagedConflict{}, Warnings: []string{},
		Revision: v.t.revision,
	}}
	edited, syntax := camconfig.ParseSectionYAML(text)
	if len(syntax) > 0 {
		out.val.Syntax = syntax
		return out, nil
	}
	out.edited = edited
	if v.schema != nil {
		issues, err := camconfig.CheckSection(v.schema, v.section, edited)
		if err != nil {
			return out, invalid("%q is not a section of the camera configuration", v.section)
		}
		out.val.SchemaChecked = true
		if len(issues) > 0 {
			out.val.Schema = issues
		}
	} else {
		out.val.Warnings = append(out.val.Warnings, "No se pudo leer el esquema del motor: solo se comprobó la sintaxis.")
	}
	out.val.Valid = len(out.val.Schema) == 0
	diff := camconfig.DiffLeaves(v.shown, edited)
	var kept, removed []camconfig.LeafChange
	for _, c := range diff {
		if c.Kind == camconfig.LeafRemoved {
			removed = append(removed, c)
		} else {
			kept = append(kept, c)
		}
	}
	if len(removed) > 0 {
		paths := make([]string, len(removed))
		for i, c := range removed {
			paths[i] = c.Path
		}
		out.val.Warnings = append(out.val.Warnings, "Frigate conserva las claves eliminadas del YAML: "+strings.Join(paths, ", ")+".")
	}
	if len(diff) == 0 {
		out.val.Warnings = append(out.val.Warnings, "No hay cambios respecto de la configuración actual.")
	}
	out.val.Changes = camconfig.SafeChanges(diff)
	if mc := camconfig.ManagedConflicts(v.managed(), kept); len(mc) > 0 {
		for i := range mc {
			mc[i].Desired, mc[i].New = camconfig.SafeValue(mc[i].EnginePath, mc[i].Desired), camconfig.SafeValue(mc[i].EnginePath, mc[i].New)
		}
		out.val.ManagedConflicts = mc
		out.val.Warnings = append(out.val.Warnings, "El cambio modifica parámetros administrados por OpenVMS; la cámara quedará con diferencias respecto de su configuración deseada.")
	}
	return out, nil
}

// ValidateCameraYAML checks an edited section without writing anything.
func (s *Service) ValidateCameraYAML(ctx context.Context, actor authz.Actor, id uuid.UUID, section, text string) (YAMLValidation, error) {
	v, err := s.openYAML(ctx, actor, id, section)
	if err != nil {
		return YAMLValidation{}, err
	}
	c, err := v.check(text)
	return c.val, err
}

// ApplyCameraYAML saves an edited section through PatchCameraFrigateConfig. An invalid edit is
// a 400, a stale revision or an unconfirmed edit of managed parameters is a conflict, and
// nothing is written in those cases. A confirmed edit of managed parameters leaves the camera
// marked as drifted (diferencia detectada) so OpenVMS never overwrites it silently.
func (s *Service) ApplyCameraYAML(ctx context.Context, actor authz.Actor, id uuid.UUID, in YAMLApplyInput) (YAMLApplyResult, error) {
	v, err := s.openYAML(ctx, actor, id, in.Section)
	if err != nil {
		return YAMLApplyResult{}, err
	}
	if in.ExpectedRevision != v.t.revision {
		return YAMLApplyResult{}, ErrRevisionConflict
	}
	c, err := v.check(in.YAML)
	if err != nil {
		return YAMLApplyResult{}, err
	}
	if len(c.val.Syntax) > 0 {
		i := c.val.Syntax[0]
		return YAMLApplyResult{}, invalid("YAML no válido (línea %d, columna %d): %s", i.Line, i.Column, i.Message)
	}
	if len(c.val.Schema) > 0 {
		i := c.val.Schema[0]
		return YAMLApplyResult{}, invalid("el YAML no cumple el esquema (%s): %s", i.Path, i.Message)
	}
	if len(c.val.ManagedConflicts) > 0 && !in.ConfirmManagedConflicts {
		return YAMLApplyResult{}, &ManagedConflictError{Conflicts: c.val.ManagedConflicts}
	}
	resolved, err := camconfig.ResolvePlaceholders(c.edited, v.shown, v.actual)
	if err != nil {
		var ve *camconfig.ValidationError
		if errors.As(err, &ve) {
			return YAMLApplyResult{}, invalid("%s", ve.Msg)
		}
		return YAMLApplyResult{}, err
	}
	base := v.actual
	if base == nil {
		base = v.shown
	}
	pruned := camconfig.PruneUnchanged(resolved, base)
	if pruned == nil {
		return YAMLApplyResult{}, invalid("no hay cambios que aplicar")
	}
	res, patchErr := s.PatchCameraFrigateConfig(ctx, actor, id, map[string]any{in.Section: pruned})
	out := YAMLApplyResult{Validation: c.val, Patch: res, Revision: v.t.revision}
	if patchErr != nil && len(res.Sections) == 0 {
		if !errors.Is(patchErr, access.ErrForbidden) && !errors.Is(patchErr, store.ErrNotFound) {
			_ = s.auditYAML(ctx, actor, v, in, c.val, res, camconfig.ResultError, camconfig.PlainFromError(patchErr))
		}
		return YAMLApplyResult{}, patchErr
	}
	result, plain := camconfig.ResultOK, camconfig.PlainError{}
	if patchErr != nil {
		result, plain = camconfig.ResultError, camconfig.PlainFromError(patchErr)
	}
	marked := len(c.val.ManagedConflicts) > 0
	marked, out.Revision, out.SyncState, err = s.recordYAMLEdit(ctx, actor, v, in, c.val, res, result, plain, marked)
	if err != nil {
		return YAMLApplyResult{}, fmt.Errorf("camera yaml: recording the outcome: %w", err)
	}
	out.Marked = marked
	return out, patchErr
}

func (s *Service) auditYAML(ctx context.Context, actor authz.Actor, v yamlView, in YAMLApplyInput, val YAMLValidation, res FrigatePatchResult, result string, plain camconfig.PlainError) error {
	_, _, _, err := s.recordYAMLEdit(ctx, actor, v, in, val, res, result, plain, false)
	return err
}

// recordYAMLEdit writes the audit row of a YAML edit and, when managed fields were changed,
// marks the camera as drifted. It returns whether it marked, and the stored revision and state.
func (s *Service) recordYAMLEdit(ctx context.Context, actor authz.Actor, v yamlView, in YAMLApplyInput, val YAMLValidation, res FrigatePatchResult, result string, plain camconfig.PlainError, mark bool) (bool, int64, string, error) {
	revision, syncState := v.t.revision, ""
	marked := false
	err := s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		if mark {
			m, rev, st, err := markYAMLDrift(ctx, q, v, in.ExpectedRevision, actor, val.ManagedConflicts)
			if err != nil {
				return err
			}
			marked, revision, syncState = m, rev, st
		}
		prev, next := map[string]any{}, map[string]any{}
		for _, c := range val.Changes {
			path := in.Section + "." + c.Path
			if c.Kind != camconfig.LeafAdded {
				prev[path] = c.From
			}
			if c.Kind != camconfig.LeafRemoved {
				next[path] = c.To
			}
		}
		conflicts := make([]string, 0, len(val.ManagedConflicts))
		for _, c := range val.ManagedConflicts {
			conflicts = append(conflicts, c.PatchPath)
		}
		sort.Strings(conflicts)
		details := map[string]any{
			"camera_id": v.t.cam.ID, "camera_name": v.t.cam.RemoteName, "server_id": v.t.srv.ID,
			"origin": camconfig.OriginYAML, "result": result, "section": in.Section,
			"expected_revision": in.ExpectedRevision, "desired_revision": revision,
			"previous_values": prev, "new_values": next, "managed_conflicts": conflicts,
			"confirmed_managed_conflicts": in.ConfirmManagedConflicts, "marked_drift": marked,
			"frigate_revision_id": res.RevisionID,
		}
		if plain.Message != "" {
			details["error_plain"] = plain
		}
		return audit(ctx, q, actor, &v.t.cam.TenantID, ActionCameraConfigChanged, "camera", v.t.cam.ID, details)
	})
	return marked, revision, syncState, err
}

// markYAMLDrift records that managed fields were edited outside the normalized model: the
// desired state stays as it was (OpenVMS does not adopt the edit) and the camera is left in
// drift with a note, so the next apply or sync shows the difference instead of hiding it.
func markYAMLDrift(ctx context.Context, q *db.Queries, v yamlView, expected int64, actor authz.Actor, conflicts []camconfig.ManagedConflict) (bool, int64, string, error) {
	if err := q.EnsureCameraConfigState(ctx, db.EnsureCameraConfigStateParams{CameraID: v.t.cam.ID, TenantID: v.t.cam.TenantID}); err != nil {
		return false, 0, "", err
	}
	cur, err := q.GetCameraConfigState(ctx, v.t.cam.ID)
	if err != nil {
		return false, 0, "", err
	}
	if cur.DesiredRevision != expected {
		// Somebody else moved the state while the engine was being written; the audit row still
		// records the edit and the next read computes the drift from the engine.
		return false, cur.DesiredRevision, cur.SyncState, nil
	}
	paths := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		paths = append(paths, c.PatchPath)
	}
	sort.Strings(paths)
	rec, err := q.RecordCameraConfigDesired(ctx, db.RecordCameraConfigDesiredParams{
		CameraID: v.t.cam.ID, Desired: cur.Desired, ExpectedRevision: expected, UpdatedBy: &actor.UserID,
	})
	if errors.Is(store.Classify(err), store.ErrNotFound) {
		return false, cur.DesiredRevision, cur.SyncState, nil
	}
	if err != nil {
		return false, 0, "", err
	}
	fin, err := q.FinishCameraConfigApply(ctx, db.FinishCameraConfigApplyParams{
		CameraID: v.t.cam.ID, DesiredRevision: rec.DesiredRevision, SyncState: camconfig.SyncDrift,
		LastError: "Editado con YAML avanzado: " + strings.Join(paths, ", "), Applied: false,
	})
	if err != nil {
		return false, 0, "", err
	}
	return true, fin.DesiredRevision, fin.SyncState, nil
}
