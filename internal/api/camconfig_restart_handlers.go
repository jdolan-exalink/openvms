package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// restartBackend is what the camera restart needs from the services; tests replace it.
type restartBackend interface {
	GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error)
	RequireCameraRestart(ctx context.Context, a authz.Actor, id uuid.UUID) error
	AuditCameraRestart(ctx context.Context, a authz.Actor, cameraID, operationID uuid.UUID, status, via string) error
	// Credentials resolves the camera's ONVIF endpoint and credentials. The restart is authorized
	// by cameras.restart alone, so the lookup runs as the system and never reaches the response.
	Credentials(ctx context.Context, cam db.GetCameraRow) (endpoint, user, pass string)
	HasRegisteredAgent(ctx context.Context, serverID uuid.UUID) bool
	RebootViaAgent(ctx context.Context, serverID uuid.UUID, req onvifdiscover.RebootRequest) error
	RebootDirect(ctx context.Context, req onvifdiscover.RebootRequest) error
}

type restartReal struct{ h *Handlers }

func (r restartReal) GetCamera(ctx context.Context, a authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	return r.h.Inv.GetCamera(ctx, a, id)
}

func (r restartReal) RequireCameraRestart(ctx context.Context, a authz.Actor, id uuid.UUID) error {
	return r.h.Inv.RequireCameraRestart(ctx, a, id)
}

func (r restartReal) AuditCameraRestart(ctx context.Context, a authz.Actor, cameraID, operationID uuid.UUID, status, via string) error {
	return r.h.Inv.AuditCameraRestart(ctx, a, cameraID, operationID, status, via)
}

func (r restartReal) Credentials(ctx context.Context, cam db.GetCameraRow) (string, string, string) {
	return r.h.resolveCameraOnvifCredentials(ctx, authz.SystemActor(), cam, nil, nil, nil)
}

func (r restartReal) HasRegisteredAgent(ctx context.Context, serverID uuid.UUID) bool {
	return r.h.Provision != nil && r.h.Provision.HasRegisteredAgent(ctx, authz.SystemActor(), serverID)
}

func (r restartReal) RebootViaAgent(ctx context.Context, serverID uuid.UUID, req onvifdiscover.RebootRequest) error {
	return r.h.Provision.ExecuteOnvifReboot(ctx, authz.SystemActor(), serverID, req)
}

// RebootDirect dials the camera from the API, which only connects to private addresses.
func (r restartReal) RebootDirect(ctx context.Context, req onvifdiscover.RebootRequest) error {
	ep, err := onvif.ParseEndpoint(req.Endpoint)
	if err != nil {
		return err
	}
	client := onvif.NewClient(onvif.PrivateNetworkTransport(), onvif.Config{Timeout: 10 * time.Second, Credentials: onvif.NewCredentials(req.Username, req.Password)})
	return onvif.NewDeviceClient(client, ep).SystemReboot(ctx)
}

func (h *Handlers) restarter() restartBackend {
	if h.camRestart != nil {
		return h.camRestart
	}
	return restartReal{h}
}

// RestartCamera reboots the camera device (ONVIF SystemReboot) through its server's agent, or with a
// direct private-network call when the server has none. It needs cameras.restart (legacy
// servers.restart holders keep it), an explicit confirm, and is audited with user, camera and result.
func (h *Handlers) RestartCamera(ctx context.Context, r gen.RestartCameraRequestObject) (gen.RestartCameraResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	be := h.restarter()
	cam, err := be.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if err := be.RequireCameraRestart(ctx, a, cam.ID); err != nil {
		return nil, err
	}
	if r.Body == nil || !r.Body.Confirm {
		return gen.RestartCamera400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "confirmation_required", Message: "Restarting the camera stops its stream; send confirm: true to proceed"}}, nil
	}
	endpoint, user, pass := be.Credentials(ctx, cam)
	if endpoint == "" {
		return gen.RestartCamera400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"}}, nil
	}
	req := onvifdiscover.RebootRequest{Endpoint: endpoint, Username: user, Password: pass}
	via := "direct"
	viaAgent := be.HasRegisteredAgent(ctx, cam.ServerID)
	if viaAgent {
		via = "agent"
	}
	op := uuid.New()
	if err := be.AuditCameraRestart(ctx, a, cam.ID, op, "started", via); err != nil {
		// Never act on a camera whose attempt cannot be recorded.
		return nil, err
	}
	if viaAgent {
		err = be.RebootViaAgent(ctx, cam.ServerID, req)
	} else {
		err = be.RebootDirect(ctx, req)
	}
	status := "succeeded"
	switch {
	case err == nil:
	case errors.Is(err, provision.ErrAgentOpUnsupported):
		status = "failed"
	case errors.Is(err, provision.ErrAgentProbeFailed):
		// The agent answered but the camera did not accept: nothing was restarted.
		status = "failed"
	default:
		// The camera may have gone down before it could answer.
		status = "outcome_unknown"
	}
	if auditErr := be.AuditCameraRestart(ctx, a, cam.ID, op, status, via); auditErr != nil && h.Log != nil {
		h.Log.ErrorContext(ctx, "audit camera restart", "camera_id", cam.ID, "error", auditErr)
	}
	switch {
	case err == nil:
		return gen.RestartCamera200JSONResponse{Restarted: true, Via: gen.CameraRestartResultVia(via)}, nil
	case errors.Is(err, provision.ErrAgentOpUnsupported):
		return gen.RestartCamera409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{Code: "agent_outdated", Message: "The server's agent is too old to restart cameras; update the agent"}}, nil
	}
	return gen.RestartCamera502JSONResponse{FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "restart_failed", Message: "The camera did not accept the restart"}}, nil
}
