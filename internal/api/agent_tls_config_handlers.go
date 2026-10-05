package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func (h *Handlers) GetServerAgentTlsConfig(ctx context.Context, r gen.GetServerAgentTlsConfigRequestObject) (gen.GetServerAgentTlsConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	config, err := h.Provision.GetAgentTLSConfig(ctx, a, uuid.UUID(r.ServerId))
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentTlsConfig200JSONResponse(agentTLSConfig(config)), nil
}

func (h *Handlers) SetServerAgentTlsConfig(ctx context.Context, r gen.SetServerAgentTlsConfigRequestObject) (gen.SetServerAgentTlsConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &provision.ValidationError{Msg: "request body is required"}
	}
	config, err := provisionAgentTLSConfig(*r.Body)
	if err != nil {
		return nil, err
	}
	config, err = h.Provision.SetAgentTLSConfig(ctx, a, uuid.UUID(r.ServerId), config)
	if err != nil {
		return nil, err
	}
	return gen.SetServerAgentTlsConfig200JSONResponse(agentTLSConfig(config)), nil
}

func (h *Handlers) DeleteServerAgentTlsConfig(ctx context.Context, r gen.DeleteServerAgentTlsConfigRequestObject) (gen.DeleteServerAgentTlsConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Provision.DeleteAgentTLSConfig(ctx, a, uuid.UUID(r.ServerId)); err != nil {
		return nil, err
	}
	return gen.DeleteServerAgentTlsConfig204Response{}, nil
}

func provisionAgentTLSConfig(in gen.ServerAgentTLSConfig) (provision.AgentTLSConfig, error) {
	if in.SecurePort < 1 || in.SecurePort > 65535 {
		return provision.AgentTLSConfig{}, &provision.ValidationError{Msg: "secure_port must be between 1 and 65535"}
	}
	out := provision.AgentTLSConfig{SecurePort: uint32(in.SecurePort), TrustMode: string(in.TrustMode)}
	if in.CaPem != nil {
		if len(*in.CaPem) > agentTLSPemMaxBytes {
			return provision.AgentTLSConfig{}, &provision.ValidationError{Msg: "ca_pem exceeds 64 KiB"}
		}
		out.CAPEM = []byte(*in.CaPem)
	}
	switch out.TrustMode {
	case provision.AgentTLSTrustSystem:
		if in.CaPem != nil {
			return provision.AgentTLSConfig{}, &provision.ValidationError{Msg: "system trust mode cannot include ca_pem"}
		}
	case provision.AgentTLSTrustCustom:
		if in.CaPem == nil || len(*in.CaPem) == 0 {
			return provision.AgentTLSConfig{}, &provision.ValidationError{Msg: "custom trust mode requires ca_pem"}
		}
	default:
		return provision.AgentTLSConfig{}, &provision.ValidationError{Msg: "trust_mode must be system or custom"}
	}
	return out, nil
}

func agentTLSConfig(in provision.AgentTLSConfig) gen.ServerAgentTLSConfig {
	out := gen.ServerAgentTLSConfig{SecurePort: int(in.SecurePort), TrustMode: gen.ServerAgentTLSConfigTrustMode(in.TrustMode)}
	if len(in.CAPEM) > 0 {
		ca := string(in.CAPEM)
		out.CaPem = &ca
	}
	return out
}
