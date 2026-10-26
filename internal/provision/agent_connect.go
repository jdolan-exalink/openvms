package provision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const maxAgentTokenLen = 256

// Why an agent could not be connected. The caller maps them to answers an operator can act on.
var (
	ErrAgentConnectUnreachable  = errors.New("the agent could not be reached at that address")
	ErrAgentConnectUnauthorized = errors.New("the agent refused the token")
	ErrAgentConnectForbidden    = errors.New("the token is valid but does not have the read scope")
	ErrAgentConnectNotAnAgent   = errors.New("the address answered, but it is not an OpenVMS agent (or it is older than 0.3)")
)

// ConnectAgentInput is an agent that already runs somewhere OpenVMS can reach: its address and a token
// created on its console (openvms-agent connection), with the extra headers a gateway in front of it may
// ask for and, for a private certificate authority, its public certificate.
type ConnectAgentInput struct {
	URL     string
	Token   string
	Headers map[string]string
	CAPEM   string
	Replace bool
}

// AgentScopes is what the token may do on the agent, as far as OpenVMS can tell without changing anything.
type AgentScopes struct {
	Read    bool
	Events  bool
	Control bool
}

// ConnectAgentResult is what the agent said about itself.
type ConnectAgentResult struct {
	BaseURL string
	Version string
	Scopes  AgentScopes
}

// AgentConnection is what is stored about a connected agent, without any secret.
type AgentConnection struct {
	Connected   bool
	BaseURL     string
	HasHeaders  bool
	HasCustomCA bool
}

type parsedConnect struct {
	endpoint AgentEndpoint
	token    string
	headers  map[string]string
	ca       string
	replace  bool
}

// parseConnectInput validates everything that can be checked without a network.
func parseConnectInput(in ConnectAgentInput) (parsedConnect, error) {
	bad := func(msg string) (parsedConnect, error) { return parsedConnect{}, &ValidationError{Msg: msg} }
	token := strings.TrimSpace(in.Token)
	if token == "" || len(token) > maxAgentTokenLen {
		return bad("the agent token is required (at most 256 characters)")
	}
	for _, r := range token {
		if r <= ' ' || r == 0x7f {
			return bad("the agent token cannot contain spaces or control characters")
		}
	}
	endpoint, err := ParseAgentEndpoint(strings.TrimSpace(in.URL))
	if err != nil {
		return bad(err.Error())
	}
	if ip := net.ParseIP(endpoint.Host); ip != nil {
		if err := checkAgentDialAddress(ip, endpoint.Scheme); err != nil {
			return bad(err.Error())
		}
	}
	if err := validateAgentRequestHeaders(in.Headers); err != nil {
		return bad(err.Error())
	}
	if in.CAPEM != "" {
		if endpoint.Scheme != "https" {
			return bad("a certificate authority only applies to an https address")
		}
		if len(in.CAPEM) > maxAgentCAPEM {
			return bad("the certificate authority bundle exceeds 64 KiB")
		}
		if _, err := buildAgentTLSConfig(endpoint.Host, []byte(in.CAPEM), false); err != nil {
			return bad("the certificate authority bundle is not valid: " + err.Error())
		}
	}
	return parsedConnect{endpoint: endpoint, token: token, headers: in.Headers, ca: in.CAPEM, replace: in.Replace}, nil
}

// verifyAgentConnection asks the agent for its metrics (needs the read scope) and probes, with
// harmless reads, whether the token also opens the readings queue and the camera registry.
func verifyAgentConnection(ctx context.Context, client AgentProbeDoer, token string) (string, AgentScopes, error) {
	status, body, err := agentGet(ctx, client, token, "/v1/metrics")
	switch {
	case err != nil:
		return "", AgentScopes{}, ErrAgentConnectUnreachable
	case status == http.StatusUnauthorized:
		return "", AgentScopes{}, ErrAgentConnectUnauthorized
	case status == http.StatusForbidden:
		return "", AgentScopes{}, ErrAgentConnectForbidden
	case status != http.StatusOK:
		return "", AgentScopes{}, ErrAgentConnectNotAnAgent
	}
	var snap agent.Snapshot
	if json.Unmarshal(body, &snap) != nil || snap.Version == "" {
		return "", AgentScopes{}, ErrAgentConnectNotAnAgent
	}
	scopes := AgentScopes{Read: true}
	if status, _, err := agentGet(ctx, client, token, "/v1/provision-lpr/events/after/0"); err == nil && status == http.StatusOK {
		scopes.Events = true
	}
	if status, _, err := agentGet(ctx, client, token, "/v1/provision-lpr/registry"); err == nil && status == http.StatusOK {
		scopes.Control = true
	}
	return snap.Version, scopes, nil
}

func agentGet(ctx context.Context, client AgentProbeDoer, token, path string) (int, []byte, error) {
	resp, err := client.Do(ctx, http.MethodGet, path, http.Header{"Authorization": []string{"Bearer " + token}}, nil)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, nil
}

// ConnectAgent registers the agent at in.URL for a server. It checks that the agent answers with that
// token before storing anything, so a typo cannot replace a working registration.
func (s *Service) ConnectAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID, in ConnectAgentInput) (ConnectAgentResult, error) {
	if serverID == uuid.Nil {
		return ConnectAgentResult{}, &ValidationError{Msg: "server is required"}
	}
	// Authorized before any request leaves the server: this endpoint makes OpenVMS call an address
	// chosen by the caller.
	if err := s.requireAgentInstall(ctx, actor, serverID); err != nil {
		return ConnectAgentResult{}, err
	}
	p, err := parseConnectInput(in)
	if err != nil {
		return ConnectAgentResult{}, err
	}
	if s.Store == nil || s.Sealer == nil {
		return ConnectAgentResult{}, errors.New("agent persistence unavailable")
	}

	config := AgentTLSConfig{TrustMode: AgentTLSTrustPlaintext}
	if p.ca != "" {
		config = AgentTLSConfig{SecurePort: uint32(p.endpoint.Port), TrustMode: AgentTLSTrustCustom, CAPEM: []byte(p.ca)} //nolint:gosec // the port was checked to be 1..65535
	}
	client, err := s.endpointClient(p.endpoint, config, onvifProbeTimeout)
	if err != nil {
		return ConnectAgentResult{}, &ValidationError{Msg: "that address cannot be used for an agent"}
	}
	version, scopes, err := verifyAgentConnection(ctx, withAgentRequestHeaders(client, p.headers), p.token)
	if err != nil {
		return ConnectAgentResult{}, err
	}

	sealedToken, err := s.Sealer.Seal([]byte(p.token), serverID[:])
	if err != nil {
		return ConnectAgentResult{}, errors.New("could not protect the agent credential")
	}
	sealedHeaders, err := sealAgentRequestHeaders(s.Sealer, serverID, p.headers)
	if err != nil {
		return ConnectAgentResult{}, &ValidationError{Msg: err.Error()}
	}
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		server, err := q.GetServer(ctx, serverID)
		if err != nil {
			return store.Classify(err)
		}
		checker, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		resource := access.Server(server.TenantID, server.SiteID, server.ID)
		if err := checker.Require(authz.ServersManage, resource); err != nil {
			return err
		}
		if err := checker.Require(authz.ServersConfigSecrets, resource); err != nil {
			return err
		}
		if _, err := q.GetServerAgentForUpdate(ctx, serverID); err == nil && !p.replace {
			return ErrAgentAlreadyProvisioned
		} else if err != nil && !errors.Is(store.Classify(err), store.ErrNotFound) {
			return err
		}
		if err := q.UpsertServerAgentConnection(ctx, db.UpsertServerAgentConnectionParams{
			ServerID: serverID, TenantID: server.TenantID, Host: p.endpoint.Host, Port: int32(p.endpoint.Port), //nolint:gosec // 1..65535
			Variant: "remote", TokenSealed: sealedToken, Version: version, BaseUrl: p.endpoint.BaseURL(), RequestHeadersSealed: sealedHeaders,
		}); err != nil {
			return err
		}
		// A certificate authority belongs to the address it was given for: a replaced agent never
		// inherits the previous one's.
		if p.ca != "" {
			ca := p.ca
			return q.UpsertServerAgentTLS(ctx, db.UpsertServerAgentTLSParams{
				ServerID: serverID, TenantID: server.TenantID, SecurePort: int32(p.endpoint.Port), TrustMode: AgentTLSTrustCustom, CaPem: &ca, //nolint:gosec // 1..65535
			})
		}
		_, err = q.DeleteServerAgentTLS(ctx, serverID)
		return err
	})
	if err != nil {
		return ConnectAgentResult{}, err
	}
	// Best effort: keep the original Frigate config on the host, without delaying the answer.
	s.ensureBackupInBackground(ctx, actor, serverID, "connect")
	return ConnectAgentResult{BaseURL: p.endpoint.BaseURL(), Version: version, Scopes: scopes}, nil
}

// DisconnectAgent forgets an agent that was connected by address. An agent installed over SSH is
// removed through the uninstall flow and is not touched here.
func (s *Service) DisconnectAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if err := s.requireAgentInstall(ctx, actor, serverID); err != nil {
		return err
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		rows, err := q.DeleteServerAgentConnection(ctx, serverID)
		if err != nil {
			return err
		}
		if rows == 0 {
			return store.ErrNotFound
		}
		_, err = q.DeleteServerAgentTLS(ctx, serverID)
		return err
	})
}

// GetAgentConnection says how the server's agent is connected, without any secret.
func (s *Service) GetAgentConnection(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentConnection, error) {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return AgentConnection{}, err
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return AgentConnection{}, nil
	}
	if err != nil {
		return AgentConnection{}, err
	}
	out := AgentConnection{Connected: row.BaseUrl != "", BaseURL: row.BaseUrl, HasHeaders: len(row.RequestHeadersSealed) != 0}
	if out.Connected {
		tlsRow, err := s.agentTLSRow(ctx, actor, serverID)
		switch {
		case err == nil:
			out.HasCustomCA = tlsRow.TrustMode == AgentTLSTrustCustom
		case !errors.Is(err, store.ErrNotFound):
			return AgentConnection{}, err // a database failure is not "no certificate authority"
		}
	}
	return out, nil
}
