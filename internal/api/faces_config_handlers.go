package api

import (
	"context"
	"errors"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

var errFaceConfigPending = errors.New("face config handlers are added in the next commit")

func (h *Handlers) ListFaceConfig(context.Context, gen.ListFaceConfigRequestObject) (gen.ListFaceConfigResponseObject, error) {
	return nil, errFaceConfigPending
}

func (h *Handlers) PatchFaceConfig(context.Context, gen.PatchFaceConfigRequestObject) (gen.PatchFaceConfigResponseObject, error) {
	return nil, errFaceConfigPending
}
