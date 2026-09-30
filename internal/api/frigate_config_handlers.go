package api

import (
	"context"
	"encoding/json"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func (h *Handlers) GetCameraFrigateConfigFull(ctx context.Context, r gen.GetCameraFrigateConfigFullRequestObject) (gen.GetCameraFrigateConfigFullResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.Inv.GetCameraFrigateConfigFull(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	return gen.GetCameraFrigateConfigFull200JSONResponse{
		CameraId: d.CameraID, CameraName: d.CameraName, ServerId: d.ServerID, FrigateVersion: d.FrigateVersion,
		Editable: d.Editable, SecretsVisible: d.SecretsVisible, Config: d.Config,
	}, nil
}

func (h *Handlers) PatchCameraFrigateConfig(ctx context.Context, r gen.PatchCameraFrigateConfigRequestObject) (gen.PatchCameraFrigateConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var sections map[string]any
	if r.Body != nil {
		sections = r.Body.Sections
	}
	res, err := h.Inv.PatchCameraFrigateConfig(ctx, a, r.CameraId, sections)
	if err != nil {
		return nil, err
	}
	out := gen.FrigatePatchResult{RevisionId: res.RevisionID, RestartRequired: res.RestartRequired, Sections: make([]gen.FrigateSectionResult, 0, len(res.Sections))}
	for _, s := range res.Sections {
		out.Sections = append(out.Sections, gen.FrigateSectionResult{Section: s.Section, AppliedLive: s.AppliedLive, RequiresRestart: s.RequiresRestart})
	}
	return gen.PatchCameraFrigateConfig200JSONResponse(out), nil
}

func (h *Handlers) GetServerFrigateConfigSchema(ctx context.Context, r gen.GetServerFrigateConfigSchemaRequestObject) (gen.GetServerFrigateConfigSchemaResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := h.Inv.GetServerFrigateSchema(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return gen.GetServerFrigateConfigSchema200JSONResponse(out), nil
}

func (h *Handlers) GetServerFrigateConfigRaw(ctx context.Context, r gen.GetServerFrigateConfigRawRequestObject) (gen.GetServerFrigateConfigRawResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	text, err := h.Inv.GetServerFrigateRaw(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.GetServerFrigateConfigRaw200JSONResponse{Yaml: text}, nil
}

func (h *Handlers) PutServerFrigateConfigRaw(ctx context.Context, r gen.PutServerFrigateConfigRawRequestObject) (gen.PutServerFrigateConfigRawResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var text string
	if r.Body != nil {
		text = r.Body.Yaml
	}
	res, err := h.Inv.SaveServerFrigateRaw(ctx, a, r.ServerId, text, deref(r.Params.Restart))
	if err != nil {
		return nil, err
	}
	return gen.PutServerFrigateConfigRaw200JSONResponse{RevisionId: res.RevisionID, RestartRequired: res.RestartRequired}, nil
}

func (h *Handlers) RollbackServerFrigateConfig(ctx context.Context, r gen.RollbackServerFrigateConfigRequestObject) (gen.RollbackServerFrigateConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.Inv.RollbackServerFrigateConfig(ctx, a, r.ServerId, r.RevisionId)
	if err != nil {
		return nil, err
	}
	return gen.RollbackServerFrigateConfig200JSONResponse{RevisionId: res.RevisionID, RestartRequired: res.RestartRequired}, nil
}

func (h *Handlers) ListServerFrigateConfigRevisions(ctx context.Context, r gen.ListServerFrigateConfigRevisionsRequestObject) (gen.ListServerFrigateConfigRevisionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	limit := 0
	if r.Params.Limit != nil {
		limit = *r.Params.Limit
	}
	revs, err := h.Inv.ListFrigateConfigRevisions(ctx, a, r.ServerId, r.Params.CameraId, r.Params.Before, limit, deref(r.Params.IncludeYaml))
	if err != nil {
		return nil, err
	}
	out := gen.ListServerFrigateConfigRevisions200JSONResponse{Items: make([]gen.FrigateConfigRevision, 0, len(revs))}
	for _, v := range revs {
		out.Items = append(out.Items, toRevision(v))
	}
	return out, nil
}

func toRevision(v inventory.FrigateConfigRevision) gen.FrigateConfigRevision {
	sections := v.Sections
	if sections == nil {
		sections = []string{}
	}
	patch := v.Patch
	if patch == nil {
		patch = map[string]any{}
	}
	return gen.FrigateConfigRevision{
		Id: v.ID, ServerId: v.ServerID, CameraId: v.CameraID, ActorUserId: v.ActorUserID, ActorName: v.ActorName,
		Kind: gen.FrigateConfigRevisionKind(v.Kind), Sections: sections, Patch: patch,
		BeforeYaml: v.BeforeYAML, AfterYaml: v.AfterYAML, CreatedAt: v.CreatedAt,
	}
}
