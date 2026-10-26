package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

const (
	bulkReasonHidden    = "sin acceso a la cámara o inexistente"
	bulkReasonForbidden = "sin permiso para configurar esta cámara"
	bulkReasonNoChange  = "ya tiene esos valores"
)

// bulkRequest is a validated bulk call.
type bulkRequest struct {
	ids     []uuid.UUID
	patch   camconfig.Patch
	section string
	origin  string
}

func (h *Handlers) parseBulk(ctx context.Context, a authz.Actor, b *gen.CameraConfigBulkRequest) (bulkRequest, error) {
	var out bulkRequest
	if b == nil {
		return out, &inventory.ValidationError{Msg: "request body is required"}
	}
	hasIDs := b.CameraIds != nil && len(*b.CameraIds) > 0
	if hasIDs == (b.GroupId != nil) {
		return out, &inventory.ValidationError{Msg: "send exactly one of camera_ids or group_id"}
	}
	patch, err := parsePatch(b.Patch)
	if err != nil {
		return out, err
	}
	section, err := camconfig.Section(patch)
	if err != nil {
		var ve *camconfig.ValidationError
		if errors.As(err, &ve) {
			return out, &inventory.ValidationError{Msg: ve.Msg}
		}
		return out, err
	}
	if hasIDs {
		out.ids = *b.CameraIds
	} else {
		ps, perr := h.mustProfileSvc()
		if perr != nil {
			return out, perr
		}
		if out.ids, err = ps.GroupCameraIDs(ctx, a, *b.GroupId); err != nil {
			return out, err
		}
	}
	seen := map[uuid.UUID]bool{}
	ids := out.ids[:0:0]
	for _, id := range out.ids {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	out.ids = ids
	if len(out.ids) == 0 {
		return out, &inventory.ValidationError{Msg: "no cameras selected"}
	}
	if len(out.ids) > camconfig.MaxBulkCameras {
		return out, &inventory.ValidationError{Msg: "too many cameras: at most 200 per call"}
	}
	out.origin = camconfig.OriginAPI
	if b.GroupId != nil {
		out.origin = camconfig.OriginGroup
	}
	if b.Origin != nil {
		out.origin = string(*b.Origin)
	}
	if out.origin != camconfig.OriginAPI && out.origin != camconfig.OriginGroup {
		return out, &inventory.ValidationError{Msg: "origin must be group or api"}
	}
	out.patch, out.section = patch, section
	return out, nil
}

func isValidation(err error) (string, bool) {
	var iv *inventory.ValidationError
	if errors.As(err, &iv) {
		return iv.Msg, true
	}
	var cv *camconfig.ValidationError
	if errors.As(err, &cv) {
		return cv.Msg, true
	}
	return "", false
}

// bulkReview classifies one camera: nothing is written. expected is the camera's desired
// revision when it was read, used by the apply that follows.
func (h *Handlers) bulkReview(ctx context.Context, a authz.Actor, id uuid.UUID, patch camconfig.Patch) (camconfig.BulkRow, int64) {
	row := camconfig.BulkRow{CameraID: id.String(), Changes: []camconfig.Change{}, Impact: []camconfig.ImpactItem{}}
	fail := func(status, reason string) (camconfig.BulkRow, int64) {
		row.Status, row.Reason = status, reason
		return row, 0
	}
	classify := func(err error) (camconfig.BulkRow, int64) {
		switch {
		case errors.Is(err, access.ErrForbidden):
			return fail(camconfig.BulkSkipped, bulkReasonForbidden)
		case errors.Is(err, store.ErrNotFound):
			return fail(camconfig.BulkSkipped, bulkReasonHidden)
		}
		if msg, ok := isValidation(err); ok {
			return fail(camconfig.BulkIncompatible, msg)
		}
		return fail(camconfig.BulkUnreachable, camconfig.SanitizeError(err))
	}
	var src camConfigSource = h.Inv
	if h.camConfig != nil {
		src = h.camConfig
	}
	ce, err := src.OpenCameraEngine(ctx, a, id)
	if err != nil {
		// A camera the actor cannot view is reported as the caller named it, nothing more.
		if errors.Is(err, access.ErrForbidden) || errors.Is(err, store.ErrNotFound) {
			return fail(camconfig.BulkSkipped, bulkReasonHidden)
		}
		return classify(err)
	}
	row.Name = ce.Camera.DisplayName
	var expected int64
	if ce.State != nil {
		expected = ce.State.DesiredRevision
	}
	if unsupported, perr := h.provisionUnsupported(ctx, a, id, patch); perr != nil {
		r, _ := classify(perr)
		r.Name = row.Name
		return r, 0
	} else if len(unsupported) > 0 {
		row.Name = ce.Camera.DisplayName
		r, _ := fail(camconfig.BulkIncompatible, strings.Join(unsupported, "; "))
		return r, 0
	}
	if err := h.guardAnalyticsModules(ctx, a, id, patch); err != nil {
		r, _ := classify(err)
		r.Name = row.Name
		return r, 0
	}
	var review camconfig.Review
	if vw, ok := h.configWriter().(videoConfigWriter); ok {
		review, err = vw.ReviewCameraConfigWith(ctx, a, id, patch, h.inventoryLinker(a))
	} else {
		review, err = h.configWriter().ReviewCameraConfig(ctx, a, id, patch)
	}
	if err != nil {
		r, _ := classify(err)
		r.Name = row.Name
		return r, 0
	}
	row.Changes, row.Impact = review.Changes, review.Impact
	switch {
	case len(review.Unsupported) > 0:
		row.Status, row.Reason = camconfig.BulkIncompatible, strings.Join(review.Unsupported, "; ")
	case len(review.Changes) == 0:
		row.Status, row.Reason = camconfig.BulkSkipped, bulkReasonNoChange
	default:
		row.Status = camconfig.BulkWillApply
	}
	return row, expected
}

func (h *Handlers) ReviewCameraConfigBulk(ctx context.Context, r gen.ReviewCameraConfigBulkRequestObject) (gen.ReviewCameraConfigBulkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	req, err := h.parseBulk(ctx, a, r.Body)
	if err != nil {
		return nil, err
	}
	rows := make([]camconfig.BulkRow, 0, len(req.ids))
	for _, id := range req.ids {
		row, _ := h.bulkReview(ctx, a, id, req.patch)
		rows = append(rows, row)
	}
	out, err := toGen[gen.CameraConfigBulkReport](camconfig.Summarize(req.section, rows))
	if err != nil {
		return nil, err
	}
	return gen.ReviewCameraConfigBulk200JSONResponse(out), nil
}

func (h *Handlers) ApplyCameraConfigBulk(ctx context.Context, r gen.ApplyCameraConfigBulkRequestObject) (gen.ApplyCameraConfigBulkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	req, err := h.parseBulk(ctx, a, r.Body)
	if err != nil {
		return nil, err
	}
	rows := make([]camconfig.BulkRow, 0, len(req.ids))
	for _, id := range req.ids {
		row, expected := h.bulkReview(ctx, a, id, req.patch)
		if row.Status == camconfig.BulkWillApply {
			row = h.bulkApplyOne(ctx, a, id, req, row, expected)
		}
		rows = append(rows, row)
	}
	out, err := toGen[gen.CameraConfigBulkReport](camconfig.Summarize(req.section, rows))
	if err != nil {
		return nil, err
	}
	return gen.ApplyCameraConfigBulk200JSONResponse(out), nil
}

// bulkApplyOne applies the section patch to one camera through the normal pipeline. Only the
// request's section is sent, so the camera's other parameters are never overwritten, and a
// failure ends this row only.
func (h *Handlers) bulkApplyOne(ctx context.Context, a authz.Actor, id uuid.UUID, req bulkRequest, row camconfig.BulkRow, expected int64) camconfig.BulkRow {
	patch, err := h.resolveCatalogSelection(ctx, a, id, req.patch)
	if err == nil {
		in := inventory.ConfigApplyInput{Patch: patch, ExpectedRevision: expected, Origin: req.origin}
		var res inventory.ConfigApplyResult
		if vw, ok := h.configWriter().(videoConfigWriter); ok {
			res, err = vw.ApplyCameraConfigWith(ctx, a, id, in, h.inventoryLinker(a))
		} else {
			res, err = h.configWriter().ApplyCameraConfig(ctx, a, id, in)
		}
		if err == nil {
			if res.Outcome.Result == camconfig.ResultOK {
				row.Status = camconfig.BulkApplied
			} else {
				row.Status, row.Reason = camconfig.BulkFailed, res.State.LastError
				if row.Reason == "" {
					row.Reason = "el cambio no pudo verificarse"
				}
			}
			return row
		}
	}
	switch {
	case errors.Is(err, access.ErrForbidden):
		row.Status, row.Reason = camconfig.BulkSkipped, bulkReasonForbidden
	case errors.Is(err, inventory.ErrRevisionConflict):
		row.Status, row.Reason = camconfig.BulkFailed, "la configuración cambió mientras se aplicaba; reintente"
	default:
		if msg, ok := isValidation(err); ok {
			row.Status, row.Reason = camconfig.BulkIncompatible, msg
		} else {
			row.Status, row.Reason = camconfig.BulkFailed, camconfig.SanitizeError(err)
		}
	}
	return row
}
