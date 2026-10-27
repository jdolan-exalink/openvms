package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"

	"github.com/google/uuid"
)

// camConfigSource loads a camera the actor may view and opens its engine; the production
// implementation is the inventory service, tests replace it.
type camConfigSource interface {
	OpenCameraEngine(ctx context.Context, a authz.Actor, id uuid.UUID) (inventory.CameraEngine, error)
}

// GetCameraConfig serves the engine-agnostic configuration model of a camera. Permission
// checks are those of viewing the camera; an unreachable engine degrades the answer instead
// of failing it.
func (h *Handlers) GetCameraConfig(ctx context.Context, r gen.GetCameraConfigRequestObject) (gen.GetCameraConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var src camConfigSource = h.Inv
	if h.camConfig != nil {
		src = h.camConfig
	}
	ce, err := src.OpenCameraEngine(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	ad := h.composedAdapter(ctx, a, ce)
	model, err := ad.Read(ctx, cameraInventory(ce))
	if err != nil {
		return nil, err
	}
	stored := inventory.StoredConfigState(ce.State)
	if ps := h.profileSvc(); ps != nil {
		ref, patch, perr := ps.CameraProfileContext(ctx, a, r.CameraId)
		if perr != nil {
			return nil, perr
		}
		stored.Inherited = patch
		model.Profile = ref
		model.Provenance = camconfig.ResolveProvenance(model, stored.Desired, ref, patch)
	}
	model.State = camconfig.ComputeState(stored, model)
	// The model's JSON tags are the contract: decode into the generated type.
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	var out gen.CameraConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("camera config: %w", err)
	}
	return gen.GetCameraConfig200JSONResponse(out), nil
}

func cameraInventory(ce inventory.CameraEngine) camconfig.Inventory {
	return inventory.CameraConfigInventory(ce.Camera, ce.ServerName)
}

// camConfigWriter reviews and applies changes of the normalized configuration; the production
// implementation is the inventory service, tests replace it.
type camConfigWriter interface {
	ReviewCameraConfig(ctx context.Context, a authz.Actor, id uuid.UUID, patch camconfig.Patch) (camconfig.Review, error)
	ApplyCameraConfig(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.ConfigApplyInput) (inventory.ConfigApplyResult, error)
}

func (h *Handlers) configWriter() camConfigWriter {
	if h.camConfigWrite != nil {
		return h.camConfigWrite
	}
	return h.Inv
}

// parsePatch turns the request's open patch object into the strict model patch: unknown or
// untranslatable fields are a 400, never ignored.
func parsePatch(in map[string]any) (camconfig.Patch, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return camconfig.Patch{}, &inventory.ValidationError{Msg: "patch is not valid JSON"}
	}
	p, err := camconfig.ParsePatch(raw)
	if err != nil {
		var ve *camconfig.ValidationError
		if errors.As(err, &ve) {
			return camconfig.Patch{}, &inventory.ValidationError{Msg: ve.Msg}
		}
		return camconfig.Patch{}, err
	}
	return p, nil
}

// reviewToGen and the helpers below carry the model's JSON shape into the generated types.
func toGen[T any](v any) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("camera config: %w", err)
	}
	return out, nil
}

// ReviewCameraConfig describes what a change would do, without side effects.
func (h *Handlers) ReviewCameraConfig(ctx context.Context, r gen.ReviewCameraConfigRequestObject) (gen.ReviewCameraConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "patch is required"}
	}
	patch, err := parsePatch(*r.Body)
	if err != nil {
		return nil, err
	}
	if unsupported, perr := h.provisionUnsupported(ctx, a, r.CameraId, patch); perr != nil {
		return nil, perr
	} else if len(unsupported) > 0 {
		out, gerr := toGen[gen.CameraConfigReview](camconfig.Review{Changes: []camconfig.Change{}, Impact: []camconfig.ImpactItem{}, Unsupported: unsupported})
		if gerr != nil {
			return nil, gerr
		}
		return gen.ReviewCameraConfig200JSONResponse(out), nil
	}
	var review camconfig.Review
	if vw, ok := h.configWriter().(videoConfigWriter); ok {
		review, err = vw.ReviewCameraConfigWith(ctx, a, r.CameraId, patch, h.inventoryLinker(a))
	} else {
		review, err = h.configWriter().ReviewCameraConfig(ctx, a, r.CameraId, patch)
	}
	if err != nil {
		return nil, err
	}
	out, err := toGen[gen.CameraConfigReview](review)
	if err != nil {
		return nil, err
	}
	return gen.ReviewCameraConfig200JSONResponse(out), nil
}

// guardAnalyticsModules rejects a patch that switches an analytics module the camera cannot
// change (not available with its engine, or configured on the camera itself): it is a 400 with
// the reason, never a silent no-op.
func (h *Handlers) guardAnalyticsModules(ctx context.Context, a authz.Actor, id uuid.UUID, p camconfig.Patch) error {
	if p.Analytics == nil || len(p.Analytics.Modules) == 0 {
		return nil
	}
	var src camConfigSource = h.Inv
	if h.camConfig != nil {
		src = h.camConfig
	}
	ce, err := src.OpenCameraEngine(ctx, a, id)
	if err != nil {
		return err
	}
	current, err := h.composedAdapter(ctx, a, ce).Read(ctx, cameraInventory(ce))
	if err != nil {
		return err
	}
	if err := camconfig.CheckModules(current, p); err != nil {
		var ve *camconfig.ValidationError
		if errors.As(err, &ve) {
			return &inventory.ValidationError{Msg: ve.Msg}
		}
		return err
	}
	return nil
}

// ApplyCameraConfig records, applies and verifies a change of the normalized configuration.
func (h *Handlers) ApplyCameraConfig(ctx context.Context, r gen.ApplyCameraConfigRequestObject) (gen.ApplyCameraConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "patch is required"}
	}
	patch, err := parsePatch(r.Body.Patch)
	if err != nil {
		return nil, err
	}
	if patch, err = h.resolveCatalogSelection(ctx, a, r.CameraId, patch); err != nil {
		return nil, err
	}
	if unsupported, perr := h.provisionUnsupported(ctx, a, r.CameraId, patch); perr != nil {
		return nil, perr
	} else if len(unsupported) > 0 {
		return nil, &inventory.ValidationError{Msg: strings.Join(unsupported, "; ")}
	}
	if err = h.guardAnalyticsModules(ctx, a, r.CameraId, patch); err != nil {
		return nil, err
	}
	in := inventory.ConfigApplyInput{Patch: patch, ExpectedRevision: r.Body.ExpectedRevision}
	if r.Body.Origin != nil {
		in.Origin = string(*r.Body.Origin)
	}
	var res inventory.ConfigApplyResult
	if vw, ok := h.configWriter().(videoConfigWriter); ok {
		res, err = vw.ApplyCameraConfigWith(ctx, a, r.CameraId, in, h.inventoryLinker(a))
	} else {
		res, err = h.configWriter().ApplyCameraConfig(ctx, a, r.CameraId, in)
	}
	if err != nil {
		return nil, err
	}
	out, err := applyResultGen(res)
	if err != nil {
		return nil, err
	}
	return gen.ApplyCameraConfig200JSONResponse(out), nil
}
