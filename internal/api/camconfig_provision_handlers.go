package api

import (
	"context"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

// provisionAgentStatus is the read-only part of the Provision service the camera config model
// uses to show the link of a Provision ISR camera; tests replace it.
type provisionAgentStatus interface {
	GetProvisionLPRStatus(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (map[string]any, error)
}

// provisionStatusTimeout bounds the agent lookup: the model must answer even when the agent
// is slow.
const provisionStatusTimeout = 3 * time.Second

func (h *Handlers) provisionAgent() provisionAgentStatus {
	if h.camProvision != nil {
		return h.camProvision
	}
	if h.Provision != nil {
		return h.Provision
	}
	return nil
}

// agentLinkSource reads one camera's entry of the edge agent's status. It never writes.
type agentLinkSource struct {
	agent provisionAgentStatus
	actor authz.Actor
}

// ProvisionLink implements camconfig.ProvisionStatusSource. A failure to ask the agent is an
// error (the integration reads as unknown); a camera the agent does not list is Registered=false.
func (s agentLinkSource) ProvisionLink(ctx context.Context, in camconfig.Inventory) (*camconfig.ProvisionLink, error) {
	if s.agent == nil {
		return nil, nil
	}
	serverID, err := uuid.Parse(in.ServerID)
	if err != nil {
		return nil, nil //nolint:nilerr // no server, no evidence
	}
	camID, _ := uuid.Parse(in.CameraID)
	ctx, cancel := context.WithTimeout(ctx, provisionStatusTimeout)
	defer cancel()
	status, err := s.agent.GetProvisionLPRStatus(ctx, s.actor, serverID)
	if err != nil {
		return nil, err
	}
	host := ""
	if u, perr := url.Parse(in.ONVIFEndpoint); perr == nil {
		host = u.Hostname()
	}
	entry := provision.FindLPRCameraStatus(status, camID, host, in.RemoteName, in.Name)
	link := &camconfig.ProvisionLink{Reachable: true, Registered: entry != nil}
	if entry != nil {
		link.Online, _ = entry["online"].(bool)
		if n, ok := entry["event_count"].(float64); ok {
			link.EventCount = int64(n)
		}
		if ts, ok := entry["last_event_time"].(string); ok && ts != "" {
			if t, perr := time.Parse(time.RFC3339Nano, ts); perr == nil && t.Year() > 1 {
				link.LastEventAt = &t
			}
		}
	}
	return link, nil
}

// composedAdapter builds the adapter that reads a camera through every engine with evidence for
// it (see camconfig.ComposedAdapter for the precedence). Only cameras that look like Provision
// ISR ever make the agent get asked.
func (h *Handlers) composedAdapter(ctx context.Context, a authz.Actor, ce inventory.CameraEngine) camconfig.ComposedAdapter {
	var ad camconfig.ComposedAdapter
	if ce.Adapter != nil {
		ad.Frigate.Engine = ce.Adapter
	}
	if h.canLinkVideo() {
		// Without the permission to use the camera's credentials the video simply is not read.
		link, lerr := h.videoLinker()(ctx, a, ce.Camera)
		if lerr != nil {
			link = camconfig.VideoLink{Reason: camconfig.VideoNoCredential}
		}
		ad.Frigate.Video = &link
	}
	ad.Provision.Status = agentLinkSource{agent: h.provisionAgent(), actor: a}
	return ad
}

// provisionOnly reports whether the camera's only engine is Provision, so its detection,
// recording and streams cannot be changed from OpenVMS.
func (h *Handlers) provisionOnly(ctx context.Context, a authz.Actor, ce inventory.CameraEngine) (bool, error) {
	in := cameraInventory(ce)
	if !camconfig.ProvisionEvidence(in) {
		return false, nil
	}
	ad := h.composedAdapter(ctx, a, ce)
	// The agent is not needed to tell the engines apart.
	ad.Provision.Status = nil
	cam, err := ad.Read(ctx, in)
	if err != nil {
		return false, err
	}
	return cam.Origin.Engine == camconfig.EngineProvision, nil
}

// provisionUnsupported answers a review or apply of a camera Provision alone serves: the
// unsupported fields with the reason, empty when the patch can go to the normal write path.
func (h *Handlers) provisionUnsupported(ctx context.Context, a authz.Actor, id uuid.UUID, p camconfig.Patch) ([]string, error) {
	var src camConfigSource
	switch {
	case h.camConfig != nil:
		src = h.camConfig
	case h.Inv != nil:
		src = h.Inv
	default:
		return nil, nil // no camera lookup wired (review/apply served by a replaced writer)
	}
	ce, err := src.OpenCameraEngine(ctx, a, id)
	if err != nil {
		return nil, err
	}
	only, err := h.provisionOnly(ctx, a, ce)
	if err != nil || !only {
		return nil, err
	}
	return camconfig.ProvisionUnsupported(p.Normalized()), nil
}
