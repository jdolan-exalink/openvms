package provision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ValidationError is a request the installer will not start.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ErrBusy means this address already has an install running.
var ErrBusy = errors.New("an install is already running for this address")

// ErrNotFound means the job is not visible to this caller.
var ErrNotFound = errors.New("install job not found")

// Request is the install form. Password lives only for the SSH session.
type Request struct {
	SiteID          uuid.UUID
	IP              string
	ServerName      string
	User            string
	Password        string
	HostKey         string
	TrustOnFirstUse bool
	AllowSystemDisk bool
}

// Service starts installs and reads their progress.
type Service struct {
	Inv    *inventory.Service
	Store  *store.Store
	Sealer *secrets.Sealer
	Log    *slog.Logger
	Dial   func(ctx context.Context, host, user, password, expectedKey string, onKey func(string)) (Conn, error)
	Binary func() ([]byte, error)

	// Authorization and transport seams prove probe denial before secret access or outbound calls.
	requireServerManage          func(context.Context, authz.Actor, uuid.UUID) error
	requireServerConfigSecrets   func(context.Context, authz.Actor, uuid.UUID) error
	requireLocalAgentPermissions func(context.Context, authz.Actor, uuid.UUID) error
	localAgentTx                 localAgentTx
	loadProbeAgent               func(context.Context, authz.Actor, uuid.UUID) (string, int32, string, AgentTLSConfig, error)
	probeRoundTripper            http.RoundTripper

	mu   sync.Mutex
	jobs map[uuid.UUID]*job
	byIP map[string]uuid.UUID
}

// New returns a provisioner that dials SSH itself.
func New(inv *inventory.Service, st *store.Store, sealer *secrets.Sealer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		Inv: inv, Store: st, Sealer: sealer, Log: log,
		Dial: dialSSH, Binary: LoadBinary,
		jobs: map[uuid.UUID]*job{}, byIP: map[string]uuid.UUID{},
	}
}

// Start authorizes the site, then runs the install in the background.
func (s *Service) Start(ctx context.Context, actor authz.Actor, in Request) (Snapshot, error) {
	in.IP = strings.TrimSpace(in.IP)
	in.ServerName = strings.TrimSpace(in.ServerName)
	in.User = strings.TrimSpace(in.User)
	if err := validate(in); err != nil {
		return Snapshot{}, err
	}
	if err := s.Inv.RequireSiteManage(ctx, actor, in.SiteID); err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	if _, ok := s.byIP[in.IP]; ok {
		s.mu.Unlock()
		return Snapshot{}, ErrBusy
	}
	j := newJob(actor.UserID, in.IP)
	s.jobs[j.snap.ID] = j
	s.byIP[in.IP] = j.snap.ID
	s.mu.Unlock()

	password := in.Password
	go s.run(context.WithoutCancel(ctx), actor, j, backgroundRequest(in), password)
	return j.snapshot(), nil
}

func backgroundRequest(in Request) Request {
	return Request{
		SiteID:          in.SiteID,
		IP:              in.IP,
		ServerName:      in.ServerName,
		User:            in.User,
		HostKey:         in.HostKey,
		TrustOnFirstUse: in.TrustOnFirstUse,
		AllowSystemDisk: in.AllowSystemDisk,
	}
}

// Get returns progress for a job this user started.
func (s *Service) Get(actor authz.Actor, id uuid.UUID) (Snapshot, error) {
	s.mu.Lock()
	j := s.jobs[id]
	s.mu.Unlock()
	if j == nil || j.owner != actor.UserID {
		return Snapshot{}, ErrNotFound
	}
	return j.snapshot(), nil
}

func (s *Service) run(ctx context.Context, actor authz.Actor, j *job, in Request, password string) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Minute)
	defer cancel()
	defer func() {
		password = ""
		s.mu.Lock()
		delete(s.byIP, in.IP)
		s.mu.Unlock()
	}()
	j.setStep(stepConnecting, stateRunning, "")
	binary, err := s.Binary()
	if err != nil {
		j.fail(stepConnecting, "agent binary: "+err.Error(), password)
		return
	}
	if in.HostKey == "" && !in.TrustOnFirstUse {
		j.fail(stepConnecting, "verified SSH host key fingerprint is required", password)
		return
	}
	conn, err := s.Dial(ctx, in.IP, in.User, password, in.HostKey, j.setHostKey)
	if err != nil {
		j.fail(stepConnecting, Scrub(err.Error(), password), password)
		s.Log.Info("provision connect failed", "job", j.snap.ID, "host", in.IP)
		return
	}
	defer conn.Close()
	s.Log.Info("provision started", "job", j.snap.ID, "host", in.IP)
	Exec(ctx, j, password, conn, binary, in.AllowSystemDisk, func(ctx context.Context, token string, variant Variant) (uuid.UUID, error) {
		return s.register(ctx, actor, in, token, variant)
	})
	snap := j.snapshot()
	s.Log.Info("provision finished", "job", snap.ID, "host", in.IP, "status", snap.Status, "variant", snap.Variant)
}

func (s *Service) register(ctx context.Context, actor authz.Actor, in Request, token string, variant Variant) (uuid.UUID, error) {
	view, err := s.Inv.CreateServer(ctx, actor, inventory.CreateServerInput{
		SiteID:        in.SiteID,
		Name:          in.ServerName,
		ImportCameras: true,
		Conn: inventory.ConnInput{
			BaseURL:  "http://" + in.IP + ":5000",
			AuthMode: frigate.AuthNone,
		},
	})
	if err != nil {
		return uuid.Nil, err
	}
	sealed, err := s.Sealer.Seal([]byte(token), view.ID[:])
	if err != nil {
		return view.ID, err
	}
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		return q.UpsertServerAgent(ctx, db.UpsertServerAgentParams{
			ServerID:    view.ID,
			TenantID:    view.TenantID,
			Host:        in.IP,
			Port:        7419,
			Variant:     string(variant),
			TokenSealed: sealed,
			Version:     agent.Version,
		})
	})
	if err != nil {
		return view.ID, err
	}
	return view.ID, nil
}

func validate(in Request) error {
	if net.ParseIP(in.IP) == nil {
		return &ValidationError{Msg: "ip must be an address"}
	}
	if in.User == "" || len(in.User) > 64 || strings.ContainsAny(in.User, " \t\r\n") {
		return &ValidationError{Msg: "ssh user is required"}
	}
	if in.Password == "" || len(in.Password) > 256 {
		return &ValidationError{Msg: "ssh password is required"}
	}
	if strings.TrimSpace(in.ServerName) == "" || len(strings.TrimSpace(in.ServerName)) > 200 {
		return &ValidationError{Msg: "server name is required and must be at most 200 characters"}
	}
	if in.HostKey == "" && !in.TrustOnFirstUse {
		return &ValidationError{Msg: "verified SSH host key fingerprint or explicit first-contact trust is required"}
	}
	if in.HostKey != "" && !validFingerprint(in.HostKey) {
		return &ValidationError{Msg: "invalid SSH host key fingerprint"}
	}
	if in.SiteID == uuid.Nil {
		return &ValidationError{Msg: "site is required"}
	}
	return nil
}

func validFingerprint(value string) bool {
	if !strings.HasPrefix(value, "SHA256:") || len(value) != len("SHA256:")+43 {
		return false
	}
	for _, r := range value[len("SHA256:"):] {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '/') {
			return false
		}
	}
	return true
}

// FetchMetrics reads the live agent on a host. token is not logged.
func FetchMetrics(ctx context.Context, host string, port int32, token string) (agent.Snapshot, error) {
	return fetchMetrics(ctx, host, port, token)
}

func formatHost(host string, port int32) string {
	return fmt.Sprintf("%s:%d", host, port)
}
