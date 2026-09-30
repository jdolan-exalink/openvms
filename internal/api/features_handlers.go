package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

// GetFeatures reports the operator-configured rollout flags. It is behind the global
// authentication middleware, so only signed-in users can read it.
func (h *Handlers) GetFeatures(context.Context, gen.GetFeaturesRequestObject) (gen.GetFeaturesResponseObject, error) {
	return gen.GetFeatures200JSONResponse{
		PersistentPlayers:     h.Features.PersistentPlayers,
		VideoSurfaceLayer:     h.Features.VideoSurfaceLayer,
		AdaptiveStreaming:     h.Features.AdaptiveStreaming,
		StreamPrewarming:      h.Features.StreamPrewarming,
		SeamlessQualitySwitch: h.Features.SeamlessQualitySwitch,
		Maps:                  h.Features.Maps,
	}, nil
}
