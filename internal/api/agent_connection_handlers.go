package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

// GetServerAgentConnection says how the server's agent is connected. It never returns a secret.
func (h *Handlers) GetServerAgentConnection(ctx context.Context, r gen.GetServerAgentConnectionRequestObject) (gen.GetServerAgentConnectionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := h.Provision.GetAgentConnection(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	out := gen.ServerAgentConnection{Connected: conn.Connected, HasHeaders: conn.HasHeaders, HasCustomCa: conn.HasCustomCA}
	if conn.BaseURL != "" {
		out.BaseUrl = &conn.BaseURL
	}
	return gen.GetServerAgentConnection200JSONResponse(out), nil
}

// ConnectServerAgent registers an agent that already runs at an address OpenVMS can reach, after the
// agent has answered with the given token.
func (h *Handlers) ConnectServerAgent(ctx context.Context, r gen.ConnectServerAgentRequestObject) (gen.ConnectServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.Token == nil {
		return nil, &provision.ValidationError{Msg: "url and token are required"}
	}
	in := provision.ConnectAgentInput{URL: r.Body.Url, Token: *r.Body.Token}
	if r.Body.Headers != nil {
		in.Headers = *r.Body.Headers
	}
	if r.Body.CaPem != nil {
		in.CAPEM = *r.Body.CaPem
	}
	if r.Body.ReplaceExisting != nil {
		in.Replace = *r.Body.ReplaceExisting
	}
	res, err := h.Provision.ConnectAgent(ctx, a, r.ServerId, in)
	if err != nil {
		return nil, err
	}
	out := gen.ServerAgentConnectResult{BaseUrl: res.BaseURL, Version: res.Version}
	out.Scopes.Read, out.Scopes.Events, out.Scopes.Control = res.Scopes.Read, res.Scopes.Events, res.Scopes.Control
	return gen.ConnectServerAgent200JSONResponse(out), nil
}

// DisconnectServerAgent forgets an agent that was connected by address.
func (h *Handlers) DisconnectServerAgent(ctx context.Context, r gen.DisconnectServerAgentRequestObject) (gen.DisconnectServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Provision.DisconnectAgent(ctx, a, r.ServerId); err != nil {
		return nil, err
	}
	return gen.DisconnectServerAgent204Response{}, nil
}
