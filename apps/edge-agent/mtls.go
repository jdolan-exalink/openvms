package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
)

// defaultStateDir holds the agent's mutable state; the credentials live in agent-mtls/ below it.
const defaultStateDir = "/var/lib/openvms-agent"

// mtlsSettings is the agent's mutual-TLS control channel: where to dial, how to verify the
// server and the managed client certificate.
type mtlsSettings struct {
	Addr       string
	CAFile     string
	ServerName string
	Manager    *mtls.Manager
}

// loadMTLS reads the opt-in mutual-TLS settings and returns nil when OPENVMS_AGENT_GRPC_ADDR
// is unset, which leaves the agent on the existing control channel.
//
//	OPENVMS_AGENT_GRPC_ADDR        host:port of the API's agent listener (enables mTLS)
//	OPENVMS_ENROLL_TOKEN[_FILE]    one-time enrollment token (only needed until a certificate is stored)
//	OPENVMS_API_URL                https base URL of the API, for enrollment
//	OPENVMS_API_CA_FILE            optional PEM bundle trusted for the API; system roots otherwise
//	OPENVMS_AGENT_STATE_DIR        state directory (default /var/lib/openvms-agent)
//	OPENVMS_CONTROL_TLS_CA_FILE / OPENVMS_CONTROL_TLS_SERVER_NAME
//	                               verify the agent listener's server certificate, as for the control channel
//
// Stored credentials always win over a configured token, so a token left in the environment
// after enrollment is harmless. Settings that only make sense with mTLS are rejected when
// OPENVMS_AGENT_GRPC_ADDR is unset, so a typo cannot silently leave the agent unenrolled.
func loadMTLS(ctx context.Context, getenv func(string) string, log *slog.Logger, now func() time.Time) (*mtlsSettings, error) {
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	addr := get("OPENVMS_AGENT_GRPC_ADDR")
	if addr == "" {
		for _, k := range []string{"OPENVMS_ENROLL_TOKEN", "OPENVMS_ENROLL_TOKEN_FILE", "OPENVMS_API_URL", "OPENVMS_API_CA_FILE", "OPENVMS_AGENT_STATE_DIR"} {
			if get(k) != "" {
				return nil, fmt.Errorf("%s requires OPENVMS_AGENT_GRPC_ADDR", k)
			}
		}
		return nil, nil
	}
	// The agent listener is TLS-only; an explicit "off" is a mistake worth stopping on.
	if v := get("OPENVMS_CONTROL_TLS"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("OPENVMS_CONTROL_TLS must be a boolean: %w", err)
		}
		if !on {
			return nil, errors.New("OPENVMS_AGENT_GRPC_ADDR needs TLS: unset OPENVMS_CONTROL_TLS or set it to true")
		}
	}
	s := &mtlsSettings{Addr: addr, CAFile: get("OPENVMS_CONTROL_TLS_CA_FILE"), ServerName: get("OPENVMS_CONTROL_TLS_SERVER_NAME")}
	// Load the server CA now so a broken file stops the agent at startup, like the control channel.
	if _, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: s.CAFile, ServerName: s.ServerName}); err != nil {
		return nil, fmt.Errorf("agent listener TLS: %w", err)
	}
	token, err := enrollToken(get)
	if err != nil {
		return nil, err
	}

	stateDir := get("OPENVMS_AGENT_STATE_DIR")
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	store := mtls.Store{Dir: filepath.Join(stateDir, "agent-mtls")}
	// Building the HTTP client and URL is deferred to the moment an enrollment is needed, so a
	// stale API setting does not stop an agent that already holds its certificate.
	enroll := func(ctx context.Context, token string) (*mtls.Credentials, error) {
		url, err := mtls.EnrollURL(get("OPENVMS_API_URL"))
		if err != nil {
			return nil, err
		}
		hc, err := mtls.NewAPIClient(get("OPENVMS_API_CA_FILE"))
		if err != nil {
			return nil, err
		}
		log.Info("enrolling with the OpenVMS API", "url", url)
		return mtls.Enroll(ctx, hc, url, token)
	}
	creds, err := mtls.Ensure(ctx, store, token, enroll, now)
	if err != nil {
		return nil, err
	}
	if creds.Expired(now()) {
		log.Error("the stored agent certificate has expired; the control channel will fail until the agent is enrolled again: create a new enrollment token for this server and set OPENVMS_ENROLL_TOKEN",
			"not_after", creds.NotAfter(), "server_id", creds.Identity.ServerID)
	}
	log.Info("agent mTLS identity loaded", "server_id", creds.Identity.ServerID, "not_after", creds.NotAfter(), "renew_at", creds.RenewalDue())
	s.Manager = mtls.NewManager(store, creds)
	s.Manager.Log = log
	return s, nil
}

// enrollToken returns the token from OPENVMS_ENROLL_TOKEN or OPENVMS_ENROLL_TOKEN_FILE (a
// file keeps it out of the process environment). Both set is ambiguous and refused.
func enrollToken(get func(string) string) (string, error) {
	token, file := get("OPENVMS_ENROLL_TOKEN"), get("OPENVMS_ENROLL_TOKEN_FILE")
	switch {
	case token != "" && file != "":
		return "", errors.New("set only one of OPENVMS_ENROLL_TOKEN and OPENVMS_ENROLL_TOKEN_FILE")
	case file != "":
		b, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			// An operator may remove the spent token file; that is only a problem when the
			// agent has no certificate yet, which Ensure reports with guidance.
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("read OPENVMS_ENROLL_TOKEN_FILE: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return token, nil
}
