package api

import (
	"context"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

// CreateServerAgentEnrollToken mints the one-time token an agent redeems at EnrollAgent.
func (h *Handlers) CreateServerAgentEnrollToken(ctx context.Context, r gen.CreateServerAgentEnrollTokenRequestObject) (gen.CreateServerAgentEnrollTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := h.AgentEnroll.CreateToken(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.CreateServerAgentEnrollToken201JSONResponse{Token: tok.Value, ExpiresAt: tok.ExpiresAt}, nil
}

// EnrollAgent is public: the agent has no session, and the token is its credential.
func (h *Handlers) EnrollAgent(ctx context.Context, r gen.EnrollAgentRequestObject) (gen.EnrollAgentResponseObject, error) {
	if r.Body == nil {
		return nil, &provision.ValidationError{Msg: "request body is required"}
	}
	res, err := h.AgentEnroll.Enroll(ctx, r.Body.Token, []byte(r.Body.CsrPem))
	if err != nil {
		return nil, err
	}
	if res != nil {
		// #nosec G118 -- asynchronous sweep outlives HTTP request
		go func() {
			sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			defer cancel()
			_, _ = h.SweepServerCamerasOnvif(sweepCtx, authz.SystemActor(), res.Record.ServerID, nil)
		}()
	}
	return gen.EnrollAgent200JSONResponse{
		CertificatePem: string(res.Issued.CertPEM),
		CaPem:          string(res.CAPEM),
		NotAfter:       res.Issued.NotAfter,
	}, nil
}

// RevokeServerAgentCertificates ends every certificate of the server's agent (servers.manage).
func (h *Handlers) RevokeServerAgentCertificates(ctx context.Context, r gen.RevokeServerAgentCertificatesRequestObject) (gen.RevokeServerAgentCertificatesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := h.AgentEnroll.RevokeServerCertificates(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.RevokeServerAgentCertificates200JSONResponse{Revoked: int(n)}, nil
}
