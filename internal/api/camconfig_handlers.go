package api

import (
	"context"
	"encoding/json"
	"fmt"

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
	var ad camconfig.FrigateAdapter
	if ce.Adapter != nil {
		ad.Engine = ce.Adapter
	}
	model, err := ad.Read(ctx, cameraInventory(ce))
	if err != nil {
		return nil, err
	}
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
	c := ce.Camera
	in := camconfig.Inventory{
		CameraID: c.ID.String(), Name: c.DisplayName, RemoteName: c.RemoteName,
		Description: c.Description, Location: c.Location, Tags: c.Tags, Enabled: c.Enabled, LPR: c.Lpr,
		LiveStream: c.LiveStream, HQStream: c.HqStream, LiveCodec: c.LiveCodec, HQCodec: c.HqCodec,
		DefaultLiveQuality: c.DefaultLiveQuality, ServerID: c.ServerID.String(), ServerName: ce.ServerName,
		ONVIFCapabilities: c.OnvifCapabilities,
	}
	if c.OnvifEndpoint != nil {
		in.ONVIFEndpoint = *c.OnvifEndpoint
	}
	if c.OnvifProfile != nil {
		in.ONVIFProfile = *c.OnvifProfile
	}
	if c.OnvifVersion != nil {
		in.ONVIFVersion = *c.OnvifVersion
	}
	return in
}
