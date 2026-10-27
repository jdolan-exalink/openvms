package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// videoLinkFunc resolves how a camera's own video (ONVIF) is reached for an actor. An error
// (access.ErrForbidden) means the actor may not use the camera's ONVIF credentials; every other
// shortfall is a link with a reason code.
type videoLinkFunc func(ctx context.Context, a authz.Actor, cam db.GetCameraRow) (camconfig.VideoLink, error)

// videoConfigWriter is a camConfigWriter that can also reach the camera's video; the inventory
// service is one.
type videoConfigWriter interface {
	ReviewCameraConfigWith(ctx context.Context, a authz.Actor, id uuid.UUID, patch camconfig.Patch, link inventory.VideoLinker) (camconfig.Review, error)
	ApplyCameraConfigWith(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.ConfigApplyInput, link inventory.VideoLinker) (inventory.ConfigApplyResult, error)
}

func (h *Handlers) videoLinker() videoLinkFunc {
	if h.camVideo != nil {
		return h.camVideo
	}
	return h.cameraVideoLink
}

// inventoryLinker adapts the actor-aware lookup to what the inventory service calls.
func (h *Handlers) inventoryLinker(a authz.Actor) inventory.VideoLinker {
	link := h.videoLinker()
	return func(ctx context.Context, cam db.GetCameraRow) (camconfig.VideoLink, error) {
		return link(ctx, a, cam)
	}
}

// canLinkVideo says whether the handlers can look at a camera's video at all: tests without
// the inventory service or an injected lookup keep their model untouched.
func (h *Handlers) canLinkVideo() bool { return h.camVideo != nil || h.Inv != nil }

// cameraVideoLink is the production lookup. Using the camera's ONVIF credentials needs what the
// ONVIF probe needs: servers.manage and servers.config.secrets on the camera's server. The
// credentials come from the same resolution as the probe and live only inside the connection;
// no response, log line or error carries them. Connections only go to private addresses.
func (h *Handlers) cameraVideoLink(ctx context.Context, a authz.Actor, cam db.GetCameraRow) (camconfig.VideoLink, error) {
	if cam.OnvifEndpoint == nil || strings.TrimSpace(*cam.OnvifEndpoint) == "" {
		return camconfig.VideoLink{Reason: camconfig.VideoNoONVIF}, nil
	}
	if h.Inv == nil {
		return camconfig.VideoLink{Reason: camconfig.VideoNoCredential}, nil
	}
	if err := h.Inv.RequireServerManageAndConfigSecrets(ctx, a, cam.ServerID); err != nil {
		return camconfig.VideoLink{}, err
	}
	endpoint, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, nil, nil, nil)
	primary, detection := camconfig.ProfileTokens(inventory.CameraConfigInventory(cam, ""))
	link := camconfig.VideoLink{PrimaryToken: primary, DetectionToken: detection}
	if endpoint == "" || user == "" {
		link.Reason = camconfig.VideoNoCredential
		return link, nil
	}
	link.Device = h.videoDeviceFor(ctx, a, cam, endpoint, user, pass)
	return link, nil
}

// agentVideoProvider is the part of the Provision service that reaches a camera's video through
// the agent of its server; tests replace it.
type agentVideoProvider interface {
	HasRegisteredAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) bool
	ExecuteOnvifVideoDevice(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error)
}

func (h *Handlers) agentVideo() agentVideoProvider {
	if h.camAgentVideo != nil {
		return h.camAgentVideo
	}
	if h.Provision != nil {
		return h.Provision
	}
	return nil
}

// videoDeviceFor chooses how the camera's video is reached. A camera whose server has an agent is
// reached through it, because the cameras of a site live on a LAN the central API cannot dial; a
// server without an agent keeps the direct dial, which only goes to private addresses.
func (h *Handlers) videoDeviceFor(ctx context.Context, a authz.Actor, cam db.GetCameraRow, endpoint, user, pass string) camconfig.VideoDevice {
	if p := h.agentVideo(); p != nil && p.HasRegisteredAgent(ctx, a, cam.ServerID) {
		return &camconfig.AgentVideoDevice{
			Endpoint: endpoint, Username: user, Password: pass,
			Call: func(ctx context.Context, req onvifdiscover.VideoDeviceRequest) (onvifdiscover.VideoDeviceResult, int, error) {
				res, status, err := p.ExecuteOnvifVideoDevice(ctx, a, cam.ServerID, req)
				if errors.Is(err, provision.ErrAgentOpUnsupported) {
					err = camconfig.ErrAgentOutdated
				}
				return res, status, err
			},
		}
	}
	return &lazyVideoDevice{endpoint: endpoint, user: user, pass: pass}
}

// lazyVideoDevice connects on first use, so a request that never needs the camera never talks
// to it. Failures to connect read as an unreachable camera.
type lazyVideoDevice struct {
	endpoint, user, pass string
	transport            http.RoundTripper // nil: private networks only

	once sync.Once
	dev  onvif.ConnDevice
	err  error
}

func (l *lazyVideoDevice) connect(ctx context.Context) (onvif.ConnDevice, error) {
	l.once.Do(func() {
		rt := l.transport
		if rt == nil {
			rt = onvif.PrivateNetworkTransport()
		}
		client := onvif.NewClient(rt, onvif.Config{Timeout: 8 * time.Second, Credentials: onvif.NewCredentials(l.user, l.pass)})
		conn, err := onvif.DiscoverConn(ctx, client, l.endpoint)
		l.dev, l.err = onvif.ConnDevice{Conn: conn}, err
	})
	return l.dev, l.err
}

func (l *lazyVideoDevice) Streams(ctx context.Context) ([]onvif.ProfileStream, error) {
	d, err := l.connect(ctx)
	if err != nil {
		return nil, err
	}
	return d.Streams(ctx)
}

func (l *lazyVideoDevice) Options(ctx context.Context, token string) (onvif.VideoOptions, error) {
	d, err := l.connect(ctx)
	if err != nil {
		return onvif.VideoOptions{}, err
	}
	return d.Options(ctx, token)
}

func (l *lazyVideoDevice) Set(ctx context.Context, token string, desired onvif.VideoDesired) (onvif.ProfileStream, error) {
	d, err := l.connect(ctx)
	if err != nil {
		return onvif.ProfileStream{}, err
	}
	return d.Set(ctx, token, desired)
}

// GetCameraVideoOptions reports the video values the camera itself says it supports.
func (h *Handlers) GetCameraVideoOptions(ctx context.Context, r gen.GetCameraVideoOptionsRequestObject) (gen.GetCameraVideoOptionsResponseObject, error) {
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
	link, err := h.videoLinker()(ctx, a, ce.Camera)
	if err != nil {
		return nil, err
	}
	rep := camconfig.BuildVideoOptions(ctx, ce.Camera.ID.String(), &link)
	out, err := toGen[gen.CameraVideoOptions](rep)
	if err != nil {
		return nil, err
	}
	return gen.GetCameraVideoOptions200JSONResponse(out), nil
}
