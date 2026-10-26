package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// camAdvancedService is the part of the inventory service the advanced tab uses; tests replace
// it. Each method enforces its own permission.
type camAdvancedService interface {
	GetCameraYAMLSection(ctx context.Context, a authz.Actor, id uuid.UUID, section string) (inventory.YAMLSection, error)
	ValidateCameraYAML(ctx context.Context, a authz.Actor, id uuid.UUID, section, text string) (inventory.YAMLValidation, error)
	ApplyCameraYAML(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.YAMLApplyInput) (inventory.YAMLApplyResult, error)
	ListCameraConfigHistory(ctx context.Context, a authz.Actor, id uuid.UUID, limit int) ([]inventory.ConfigHistoryEntry, error)
	RollbackCameraConfig(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.RollbackInput, link inventory.VideoLinker) (inventory.RollbackResult, error)
	GetCameraAdvanced(ctx context.Context, a authz.Actor, id uuid.UUID) (inventory.CameraAdvanced, error)
}

func (h *Handlers) advancedSvc() camAdvancedService {
	if h.camAdvanced != nil {
		return h.camAdvanced
	}
	return h.Inv
}

func yamlValidationGen(v inventory.YAMLValidation) (gen.CameraYamlValidation, error) {
	return toGen[gen.CameraYamlValidation](map[string]any{
		"section": v.Section, "valid": v.Valid, "schema_checked": v.SchemaChecked, "syntax": v.Syntax, "schema": v.Schema,
		"changes": v.Changes, "managed_conflicts": v.ManagedConflicts, "warnings": v.Warnings, "revision": v.Revision,
	})
}

// GetCameraFrigateYaml serves one section of the camera's Frigate configuration as YAML.
func (h *Handlers) GetCameraFrigateYaml(ctx context.Context, r gen.GetCameraFrigateYamlRequestObject) (gen.GetCameraFrigateYamlResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sec, err := h.advancedSvc().GetCameraYAMLSection(ctx, a, r.CameraId, r.Params.Section)
	if err != nil {
		return nil, err
	}
	out, err := toGen[gen.CameraYamlSection](map[string]any{
		"camera_id": sec.CameraID, "section": sec.Section, "yaml": sec.YAML, "exists": sec.Exists,
		"secrets_visible": sec.SecretsVisible, "managed_paths": sec.ManagedPaths, "revision": sec.Revision,
	})
	if err != nil {
		return nil, err
	}
	return gen.GetCameraFrigateYaml200JSONResponse(out), nil
}

// ValidateCameraFrigateYaml checks an edited section without saving it.
func (h *Handlers) ValidateCameraFrigateYaml(ctx context.Context, r gen.ValidateCameraFrigateYamlRequestObject) (gen.ValidateCameraFrigateYamlResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "section and yaml are required"}
	}
	val, err := h.advancedSvc().ValidateCameraYAML(ctx, a, r.CameraId, r.Body.Section, r.Body.Yaml)
	if err != nil {
		return nil, err
	}
	out, err := yamlValidationGen(val)
	if err != nil {
		return nil, err
	}
	return gen.ValidateCameraFrigateYaml200JSONResponse(out), nil
}

// ApplyCameraFrigateYaml saves an edited section through the sectioned Frigate PATCH.
func (h *Handlers) ApplyCameraFrigateYaml(ctx context.Context, r gen.ApplyCameraFrigateYamlRequestObject) (gen.ApplyCameraFrigateYamlResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "section, yaml and expected_revision are required"}
	}
	res, err := h.advancedSvc().ApplyCameraYAML(ctx, a, r.CameraId, inventory.YAMLApplyInput{
		Section: r.Body.Section, YAML: r.Body.Yaml, ExpectedRevision: r.Body.ExpectedRevision,
		ConfirmManagedConflicts: deref(r.Body.ConfirmManagedConflicts),
	})
	if err != nil {
		return nil, err
	}
	val, err := yamlValidationGen(res.Validation)
	if err != nil {
		return nil, err
	}
	sections := make([]gen.FrigateSectionResult, 0, len(res.Patch.Sections))
	for _, s := range res.Patch.Sections {
		sections = append(sections, gen.FrigateSectionResult{Section: s.Section, AppliedLive: s.AppliedLive, RequiresRestart: s.RequiresRestart})
	}
	return gen.ApplyCameraFrigateYaml200JSONResponse(gen.CameraYamlApplyResult{
		Validation:  val,
		Frigate:     gen.FrigatePatchResult{RevisionId: res.Patch.RevisionID, RestartRequired: res.Patch.RestartRequired, Sections: sections},
		MarkedDrift: res.Marked, Revision: res.Revision, SyncState: res.SyncState,
	}), nil
}

// ListCameraConfigHistory lists the recent configuration changes of a camera.
func (h *Handlers) ListCameraConfigHistory(ctx context.Context, r gen.ListCameraConfigHistoryRequestObject) (gen.ListCameraConfigHistoryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	limit := 0
	if r.Params.Limit != nil {
		limit = *r.Params.Limit
	}
	entries, err := h.advancedSvc().ListCameraConfigHistory(ctx, a, r.CameraId, limit)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCameraConfigHistory200JSONResponse, 0, len(entries))
	for _, e := range entries {
		g, err := historyGen(e)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func historyGen(e inventory.ConfigHistoryEntry) (gen.CameraConfigHistoryEntry, error) {
	changes := make([]map[string]any, 0, len(e.Changes))
	for _, c := range e.Changes {
		changes = append(changes, map[string]any{
			"path": c.Path, "from": camconfig.SafeValue(c.Path, c.From), "to": camconfig.SafeValue(c.Path, c.To),
		})
	}
	m := map[string]any{
		"id": e.ID, "time": e.At.UTC().Format(time.RFC3339Nano), "user_id": e.UserID, "user": e.UserName,
		"origin": e.Origin, "result": e.Result, "section": e.Section, "changes": changes,
		"rollbackable": e.Rollbackable, "rollback_reason": e.RollbackReason,
	}
	if e.RollbackOf != 0 {
		m["rollback_of"] = e.RollbackOf
	}
	if e.RolledBackBy != 0 {
		m["rolled_back_by"] = e.RolledBackBy
	}
	if e.Error != nil {
		m["error"] = e.Error
	}
	return toGen[gen.CameraConfigHistoryEntry](m)
}

// RollbackCameraConfig restores the old values of a recorded change through the normal apply.
func (h *Handlers) RollbackCameraConfig(ctx context.Context, r gen.RollbackCameraConfigRequestObject) (gen.RollbackCameraConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "expected_revision is required"}
	}
	in := inventory.RollbackInput{ExpectedRevision: r.Body.ExpectedRevision}
	if r.Body.AuditId != nil {
		in.AuditID = *r.Body.AuditId
	}
	res, err := h.advancedSvc().RollbackCameraConfig(ctx, a, r.CameraId, in, h.inventoryLinker(a))
	if err != nil {
		return nil, err
	}
	out, err := applyResultGen(res.Apply)
	if err != nil {
		return nil, err
	}
	return gen.RollbackCameraConfig200JSONResponse(gen.CameraConfigRollbackResult{RolledBack: res.AuditID, Result: out}), nil
}

// applyResultGen is the apply result of the normalized config as the contract shows it, with
// the plain error next to the unchanged last_error.
func applyResultGen(res inventory.ConfigApplyResult) (gen.CameraConfigApplyResult, error) {
	out := gen.CameraConfigApplyResult{Result: gen.CameraConfigApplyResultResult(res.Outcome.Result)}
	var err error
	if out.State, err = toGen[gen.CameraConfigState](res.State); err != nil {
		return out, err
	}
	if out.Review, err = toGen[gen.CameraConfigReview](res.Review); err != nil {
		return out, err
	}
	if p := res.Outcome.Plain; p.Message != "" {
		out.PlainError = &gen.CameraPlainError{Message: p.Message, Technical: p.Technical}
	}
	return out, nil
}

// GetCameraAdvanced serves the read-only technical panels of a camera.
func (h *Handlers) GetCameraAdvanced(ctx context.Context, r gen.GetCameraAdvancedRequestObject) (gen.GetCameraAdvancedResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	adv, err := h.advancedSvc().GetCameraAdvanced(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	link := map[string]any{"reachable": false, "registered": false, "online": false, "event_count": 0}
	if adv.Provision.Evidence {
		if l, lerr := (agentLinkSource{agent: h.provisionAgent(), actor: a}).ProvisionLink(ctx, adv.Inventory); lerr == nil && l != nil {
			link = map[string]any{
				"reachable": l.Reachable, "registered": l.Registered, "online": l.Online, "event_count": l.EventCount,
				"last_event_at": l.LastEventAt,
			}
		}
	}
	profiles := make([]map[string]any, 0, len(adv.ONVIF.Profiles))
	for _, p := range adv.ONVIF.Profiles {
		profiles = append(profiles, map[string]any{
			"token": p.Token, "name": p.Name, "encoding": p.Encoding, "width": p.Width, "height": p.Height,
			"fps": p.FPS, "bitrate": p.Bitrate, "stream_uri": p.StreamURI,
		})
	}
	f, o, p := adv.Frigate, adv.ONVIF, adv.Provision
	out, err := toGen[gen.CameraAdvanced](map[string]any{
		"camera_id": adv.CameraID,
		"frigate": map[string]any{
			"available": f.Available, "reason": f.Reason, "version": f.Version, "sections": f.Sections,
			"managed_sections": f.ManagedSections, "desired_revision": f.DesiredRevision,
			"applied_revision": f.AppliedRevision, "sync_state": f.SyncState,
		},
		"onvif": map[string]any{
			"configured": o.Configured, "endpoint": o.Endpoint, "version": o.Version, "profile": o.Profile,
			"manufacturer": o.Manufacturer, "model": o.Model, "firmware": o.Firmware, "profiles": profiles,
			"capabilities": o.Capabilities, "services": o.Services, "reported": o.Reported,
		},
		"provision": map[string]any{
			"evidence": p.Evidence, "tags": p.Tags, "manufacturer": p.Manufacturer, "model": p.Model, "link": link,
		},
	})
	if err != nil {
		return nil, err
	}
	return gen.GetCameraAdvanced200JSONResponse(out), nil
}
