package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	localAgentSchemaVersion = 32
	localAgentTokenMaxBytes = 1024
	localAgentCAMaxBytes    = 64 << 10
)

func runLocalAgentRegister(ctx context.Context, args []string, dbURL string, out io.Writer) error {
	fs := flag.NewFlagSet("agent-register", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverText := fs.String("server-id", "", "existing server UUID")
	sessionFile := fs.String("session-file", "", "private file containing an active OpenVMS session credential")
	tokenFile := fs.String("token-file", "", "private local agent token file")
	caFile := fs.String("ca-file", "", "public PEM trust anchor for the agent TLS certificate")
	host := fs.String("agent-ipv4", "", "fixed registered agent IPv4 address")
	httpPort := fs.Int("http-port", 0, "agent HTTP metrics port")
	httpsPort := fs.Int("https-port", 0, "agent HTTPS probe port")
	if err := fs.Parse(args); err != nil {
		return errors.New("invalid agent registration arguments")
	}
	if fs.NArg() != 0 || *serverText == "" || *sessionFile == "" || *tokenFile == "" || *caFile == "" || *host == "" || *httpPort < 1 || *httpPort > 65535 || *httpsPort < 1 || *httpsPort > 65535 || *httpPort == *httpsPort {
		return errors.New("server ID, session file, token file, CA file, IPv4, and distinct valid HTTP/HTTPS ports are required")
	}
	serverID, err := uuid.Parse(*serverText)
	if err != nil || serverID == uuid.Nil {
		return errors.New("server ID must be a UUID")
	}
	ip, err := netip.ParseAddr(*host)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.String() != *host {
		return errors.New("agent address must be a canonical IPv4 literal")
	}
	cfg, err := config.Load("vmsctl")
	if err != nil {
		return errors.New("could not load local agent registration configuration")
	}

	// This command deliberately does not use open(), which applies migrations.
	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return errors.New("could not connect to the existing local database")
	}
	defer pool.Close()
	version, err := postgres.SchemaVersion(ctx, pool)
	if err != nil || version < localAgentSchemaVersion {
		return errors.New("agent schema migrations 00031 and 00032 must be applied explicitly before registration")
	}
	sealer, err := secrets.FromEnv()
	if err != nil {
		return errors.New("agent registration requires the configured application master key")
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
	inv := inventory.New(st, sealer, nil)
	service := provision.New(inv, st, sealer, nil)
	input := provision.LocalAgentRegistration{
		ServerID: serverID, Host: *host, HTTPPort: uint32(*httpPort), SecurePort: uint32(*httpsPort),
	}
	return registerLocalAgentFromFiles(ctx, input, *tokenFile, *caFile,
		func() error { return service.RequireLocalAgentRegistration(ctx, actor, serverID) },
		func(in provision.LocalAgentRegistration, token string) error {
			if err := service.RegisterLocalAgent(ctx, actor, identity.HashToken(sessionToken), cfg.SessionIdle, in, token); err != nil {
				if errors.Is(err, provision.ErrAgentAlreadyProvisioned) {
					return err
				}
				return errors.New("local agent registration failed")
			}
			return nil
		}, out)
}

func localAgentRegistrationActor(ctx context.Context, st *store.Store, token string, idle time.Duration) (authz.Actor, error) {
	if token == "" {
		return authz.Actor{}, errors.New("active session is required")
	}
	var row db.GetActorBySessionHashRow
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		row, err = q.GetActorBySessionHash(ctx, localAgentSessionQuery(token, idle))
		if err != nil {
			return store.Classify(err)
		}
		return q.TouchSession(ctx, row.SessionID)
	})
	if err != nil {
		return authz.Actor{}, errors.New("operator was not found or is unavailable")
	}
	if row.TenantID == nil || *row.TenantID == uuid.Nil {
		return authz.Actor{}, errors.New("operator was not found or is unavailable")
	}
	return authz.Actor{UserID: row.ID, Username: row.Username, TenantID: row.TenantID, SessionID: &row.SessionID}, nil
}

func localAgentSessionQuery(token string, idle time.Duration) db.GetActorBySessionHashParams {
	return localAgentSessionHashQuery(identity.HashToken(token), idle)
}

func localAgentSessionHashQuery(hash []byte, idle time.Duration) db.GetActorBySessionHashParams {
	return db.GetActorBySessionHashParams{TokenHash: hash, IdleSeconds: idle.Seconds()}
}

func registerLocalAgentFromFiles(_ context.Context, in provision.LocalAgentRegistration, tokenPath, caPath string, authorize func() error, register func(provision.LocalAgentRegistration, string) error, out io.Writer) error {
	if err := authorize(); err != nil {
		return err
	}
	token, err := readLocalAgentTokenFile(tokenPath)
	if err != nil {
		return err
	}
	ca, err := readLocalAgentCAFile(caPath)
	if err != nil {
		return err
	}
	in.CAPEM = ca
	if err := register(in, token); err != nil {
		return err
	}
	fmt.Fprintf(out, "registered local agent for server %s at %s (HTTP %d, HTTPS %d; custom CA trust)\n", in.ServerID, in.Host, in.HTTPPort, in.SecurePort)
	return nil
}

func readLocalAgentTokenFile(path string) (string, error) {
	data, err := readLocalAgentFile(path, localAgentTokenMaxBytes, true)
	if err != nil {
		return "", errors.New("token file must be a private regular file with restrictive permissions")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || len(token) > localAgentTokenMaxBytes || strings.ContainsAny(token, "\r\n\t ") {
		return "", errors.New("token file does not contain one bounded token")
	}
	return token, nil
}

func readLocalAgentCAFile(path string) ([]byte, error) {
	data, err := readLocalAgentFile(path, localAgentCAMaxBytes, false)
	if err != nil {
		return nil, errors.New("CA file must be a bounded regular file")
	}
	return data, nil
}

func readLocalAgentFile(path string, maxBytes int64, private bool) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("file path is required")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("file is not a regular file")
	}
	if private && before.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("private file permissions are too broad")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.New("file changed during validation")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, errors.New("file exceeds its size limit")
	}
	return data, nil
}
