package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
)

const (
	// Migrations 00074 and 00075 add the base URL and the sealed headers of a connected agent.
	connectAgentSchemaVersion = 75
	connectAgentHeadersMax    = 16 << 10
)

// runAgentConnect is `vmsctl agent-connect`: register an agent that already runs at an address OpenVMS
// reaches, as an authenticated operator, with the same checks as the screen and the API. The token, the
// gateway headers and the certificate authority come from files so no secret appears in a command line.
func runAgentConnect(ctx context.Context, args []string, dbURL string, out io.Writer) error {
	fs := flag.NewFlagSet("agent-connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverText := fs.String("server-id", "", "existing server UUID")
	sessionFile := fs.String("session-file", "", "private file containing an active OpenVMS session credential")
	rawURL := fs.String("url", "", "where OpenVMS reaches the agent: http://host:port, or https://name if it is published")
	tokenFile := fs.String("token-file", "", "private file with the agent token (openvms-agent connection)")
	headersFile := fs.String("headers-file", "", "optional private JSON file {\"Header\": \"value\"} with the headers a gateway in front of the agent asks for")
	caFile := fs.String("ca-file", "", "optional PEM file with the certificate authority of a private https agent")
	replace := fs.Bool("replace", false, "replace an agent that is already registered for the server")
	if err := fs.Parse(args); err != nil {
		return errors.New("invalid agent-connect arguments")
	}
	if fs.NArg() != 0 || *serverText == "" || *sessionFile == "" || *rawURL == "" || *tokenFile == "" {
		return errors.New("server ID, session file, URL and token file are required")
	}
	serverID, err := uuid.Parse(*serverText)
	if err != nil || serverID == uuid.Nil {
		return errors.New("server ID must be a UUID")
	}
	cfg, err := config.Load("vmsctl")
	if err != nil {
		return errors.New("could not load the configuration")
	}
	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return errors.New("could not connect to the database")
	}
	defer pool.Close()
	version, err := postgres.SchemaVersion(ctx, pool)
	if err != nil || version < connectAgentSchemaVersion {
		return errors.New("the database migrations through 00075 must be applied first (vmsctl migrate)")
	}
	sealer, err := secrets.FromEnv()
	if err != nil {
		return errors.New("connecting an agent requires the configured application master key")
	}
	st := &store.Store{Pool: pool}
	sessionToken, err := readLocalAgentTokenFile(*sessionFile)
	if err != nil {
		return errors.New("session file must be a private regular file containing one active session credential")
	}
	actor, err := localAgentRegistrationActor(ctx, st, sessionToken, cfg.SessionIdle)
	if err != nil {
		return err
	}
	service := provision.New(inventory.New(st, sealer, nil), st, sealer, nil)
	return connectAgentFromFiles(ctx, serverID, *rawURL, *tokenFile, *headersFile, *caFile, *replace,
		func(in provision.ConnectAgentInput) (provision.ConnectAgentResult, error) {
			return service.ConnectAgent(ctx, actor, serverID, in)
		}, out)
}

// connectAgentFromFiles reads the secrets, calls connect and reports what the agent said, in plain
// words. Nothing is called before every file has been read and checked.
func connectAgentFromFiles(_ context.Context, serverID uuid.UUID, rawURL, tokenPath, headersPath, caPath string, replace bool,
	connect func(provision.ConnectAgentInput) (provision.ConnectAgentResult, error), out io.Writer) error {
	token, err := readLocalAgentTokenFile(tokenPath)
	if err != nil {
		return err
	}
	in := provision.ConnectAgentInput{URL: rawURL, Token: token, Replace: replace}
	if headersPath != "" {
		data, err := readLocalAgentFile(headersPath, connectAgentHeadersMax, true)
		if err != nil {
			return errors.New("headers file must be a private regular file with restrictive permissions")
		}
		if err := json.Unmarshal(data, &in.Headers); err != nil {
			return errors.New("headers file must be a JSON object of header names to string values")
		}
	}
	if caPath != "" {
		ca, err := readLocalAgentCAFile(caPath)
		if err != nil {
			return err
		}
		in.CAPEM = string(ca)
	}
	res, err := connect(in)
	switch {
	case err == nil:
	case errors.Is(err, provision.ErrAgentConnectUnreachable):
		return errors.New("OpenVMS could not reach the agent at that address (check the address, the VPN or tunnel, and that the agent is running)")
	case errors.Is(err, provision.ErrAgentConnectUnauthorized):
		return errors.New("the agent refused the token (create a new one on the agent host)")
	case errors.Is(err, provision.ErrAgentConnectForbidden):
		return errors.New("the token is valid but does not have the read scope")
	case errors.Is(err, provision.ErrAgentConnectNotAnAgent):
		return errors.New("that address answered but it is not an OpenVMS agent")
	case errors.Is(err, provision.ErrAgentAlreadyProvisioned):
		return errors.New("the server already has an agent registered; pass -replace to replace it")
	default:
		var ve *provision.ValidationError
		if errors.As(err, &ve) {
			return errors.New(ve.Msg)
		}
		return errors.New("connecting the agent failed")
	}
	fmt.Fprintf(out, "connected the agent of server %s at %s (version %s)\n", serverID, res.BaseURL, res.Version)
	fmt.Fprintf(out, "the token allows: %s\n", scopesText(res.Scopes))
	return nil
}

func scopesText(s provision.AgentScopes) string {
	text := "read"
	if s.Events {
		text += ", events"
	} else {
		text += " (NOT events: plate readings will not be collected)"
	}
	if s.Control {
		text += ", control"
	} else {
		text += " (NOT control: ONVIF operations and the camera registry may not work)"
	}
	return text
}
