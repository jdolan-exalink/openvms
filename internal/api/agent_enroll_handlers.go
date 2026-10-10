package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

// CreateServerAgentEnrollToken mints the one-time token an agent redeems at EnrollAgent.
func (h *Handlers) CreateServerAgentEnrollToken(ctx context.Context, r gen.CreateServerAgentEnrollTokenRequestObject) (gen.CreateServerAgentEnrollTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := h.AgentEnroll.CreateToken(ctx, a, uuid.UUID(r.ServerId))
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
	return gen.EnrollAgent200JSONResponse{
		CertificatePem: string(res.Issued.CertPEM),
		CaPem:          string(res.CAPEM),
		NotAfter:       res.Issued.NotAfter,
	}, nil
}
