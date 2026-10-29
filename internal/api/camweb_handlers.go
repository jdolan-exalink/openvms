package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camweb"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionCameraWebSession audits the opening of a camera web view session.
const ActionCameraWebSession = "CAMERA_WEB_SESSION"

// NewCameraWebService wires the camera web view to the inventory permissions and to the agent client
// of the provisioning service. A session needs cameras.web.access on the camera; the legacy pair
// servers.manage plus configuration secrets on its server stays equivalent.
func NewCameraWebService(inv *inventory.Service, prov *provision.Service) *camweb.Service {
	ready := func(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
		switch err := prov.CameraWebAgentReady(ctx, actor, serverID); {
		case err == nil:
			return nil
		case errors.Is(err, provision.ErrAgentUnprovisioned):
			return camweb.ErrNoAgent
		default:
			return camweb.ErrUnreachable
		}
	}
	return camweb.NewService(camweb.Config{}, cameraWebForwarder{prov}, func(ctx context.Context, actor authz.Actor, cam camweb.Camera) error {
		return inv.RequireCameraWebAccess(ctx, actor, cam.ID)
	}, ready)
}

type cameraWebForwarder struct{ p *provision.Service }

func (f cameraWebForwarder) Forward(ctx context.Context, actor authz.Actor, serverID uuid.UUID, method string, header http.Header, body io.Reader) (*http.Response, error) {
	return f.p.CameraWebForward(ctx, actor, serverID, method, header, body)
}

func (h *Handlers) CreateCameraWebSession(ctx context.Context, r gen.CreateCameraWebSessionRequestObject) (gen.CreateCameraWebSessionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if h.CameraWeb == nil {
		return gen.CreateCameraWebSession404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "camera_web_unavailable", Message: "the camera web view is not enabled"}}, nil
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	// The address comes from the camera record only; the caller picks scheme and port.
	host := ""
	if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
		if u, perr := url.Parse(*cam.OnvifEndpoint); perr == nil {
			host = u.Hostname()
		}
	}
	_, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, nil, nil, nil)
	if host == "" {
		if hosts, herr := h.Inv.GetServerCameraStreamHosts(ctx, a, cam.ServerID); herr == nil {
			info := findStreamInfo(hosts, cam.RemoteName, cam.DisplayName)
			host = info.Host
			if user == "" && info.Username != "" {
				user = info.Username
				pass = info.Password
			}
		}
	}
	return h.openCameraWeb(ctx, a, camweb.Camera{ID: cam.ID, ServerID: cam.ServerID, Host: host, Username: user, Password: pass}, r.Body)
}

func (h *Handlers) openCameraWeb(ctx context.Context, a authz.Actor, cam camweb.Camera, body *gen.CreateCameraWebSessionJSONRequestBody) (gen.CreateCameraWebSessionResponseObject, error) {
	scheme, port := "http", 0
	if body != nil {
		if body.Scheme != nil {
			scheme = string(*body.Scheme)
		}
		if body.Port != nil {
			port = *body.Port
		}
	}
	if port == 0 {
		port = 80
		if scheme == "https" {
			port = 443
		}
	}
	opened, err := h.CameraWeb.Open(ctx, a, cam, scheme, port)
	if errors.Is(err, access.ErrForbidden) {
		h.auditCameraWebSession(ctx, a, cam, scheme, port, "denied")
	}
	switch {
	case err == nil:
	case errors.Is(err, camweb.ErrBadRequest):
		return gen.CreateCameraWebSession400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_scheme_or_port", Message: "scheme must be http or https and the port a camera web port"}}, nil
	case errors.Is(err, camweb.ErrNoAddress):
		return gen.CreateCameraWebSession404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: "camera_address_unknown", Message: "the camera has no private IPv4 address to open (set its ONVIF endpoint)"}}, nil
	case errors.Is(err, camweb.ErrNoAgent):
		return gen.CreateCameraWebSession409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{Code: "agent_unprovisioned", Message: "the camera's server has no agent"}}, nil
	case errors.Is(err, camweb.ErrAgentUnsupported):
		return gen.CreateCameraWebSession409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{Code: "agent_unsupported", Message: "update the agent to open camera web interfaces"}}, nil
	case errors.Is(err, camweb.ErrUnreachable):
		return gen.CreateCameraWebSession502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "camera_unreachable", Message: "the agent could not reach the camera web interface; check the camera address and port"}}, nil
	default:
		return nil, err
	}
	h.auditCameraWebSession(ctx, a, cam, scheme, port, "opened")
	return gen.CreateCameraWebSession200JSONResponse{Url: opened.URL, ExpiresAt: opened.ExpiresAt}, nil
}

// auditCameraWebSession records who tried to open which camera, how and with what result (PRD §41). Credentials and cookies never pass here.
func (h *Handlers) auditCameraWebSession(ctx context.Context, a authz.Actor, cam camweb.Camera, scheme string, port int, result string) {
	if h.Inv == nil || h.Inv.Store == nil {
		return
	}
	details, err := json.Marshal(map[string]any{"scheme": scheme, "port": port, "server_id": cam.ServerID, "result": result, "origin": "ui"})
	if err != nil {
		return
	}
	camID := cam.ID
	werr := h.Inv.Store.Tx(context.WithoutCancel(ctx), store.ScopeFor(a), func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: a.TenantID, ActorID: &a.UserID, ActorName: a.Username, Action: ActionCameraWebSession,
			TargetType: "camera", TargetID: &camID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: details,
		})
	})
	if werr != nil && h.Log != nil {
		h.Log.ErrorContext(ctx, "camera web session audit", "error", werr)
	}
}
