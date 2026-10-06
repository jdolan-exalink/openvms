package provision

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	agentInstallHTTPSPort    = 7443
	agentInstallMaxJobs      = 128
	agentInstallJobRetention = time.Hour
	agentInstallJobTimeout   = 10 * time.Minute
)

var (
	ErrAgentInstallBusy        = errors.New("agent install already running for server")
	ErrAgentInstallJobNotFound = errors.New("agent install job not found")
)

type AgentInstallStartRequest struct {
	Host     string
	Port     uint16
	Password string
	HostKey  string
}

func (request AgentInstallStartRequest) String() string {
	return fmt.Sprintf("AgentInstallStartRequest{host:%q port:%d host_key_fingerprint:%q password:[redacted]}", request.Host, request.Port, request.HostKey)
}
func (request AgentInstallStartRequest) GoString() string { return request.String() }
func (request AgentInstallStartRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Host     string `json:"host,omitempty"`
		Port     uint16 `json:"port,omitempty"`
		Password string `json:"password"`
	}{Host: request.Host, Port: request.Port, Password: "[redacted]"})
}

type AgentInstallJob struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Stage   string    `json:"stage"`
	Message string    `json:"message,omitempty"`
}

func (job AgentInstallJob) String() string {
	return fmt.Sprintf("AgentInstallJob{ID:%s Status:%s Stage:%s Message:%q}", job.ID, job.Status, job.Stage, job.Message)
}
func (job AgentInstallJob) GoString() string { return job.String() }

type agentInstallJob struct {
	mu        sync.Mutex
	ownerID   uuid.UUID
	serverID  uuid.UUID
	createdAt time.Time
	updatedAt time.Time
	snapshot  AgentInstallJob
}

func (job *agentInstallJob) update(status, stage, message string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.snapshot.Status, job.snapshot.Stage, job.snapshot.Message = status, stage, message
	job.updatedAt = time.Now()
}
func (job *agentInstallJob) read() AgentInstallJob {
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.snapshot
}

func validateAgentInstallStartRequest(in AgentInstallStartRequest) error {
	ip, err := netip.ParseAddr(in.Host)
	if err != nil || !ip.Is4() || ip.String() != in.Host || in.Port == 0 || len(in.Password) == 0 || len(in.Password) > 4096 || !validFingerprint(in.HostKey) {
		return &ValidationError{Msg: "invalid agent install request"}
	}
	return nil
}

// StartServerAgentInstall authorizes before reading agent rows or trusted artifacts, then
// creates a bounded memory-only job. Passwords are captured only by the worker closure.
func (s *Service) StartServerAgentInstall(ctx context.Context, actor authz.Actor, serverID uuid.UUID, in AgentInstallStartRequest) (AgentInstallJob, error) {
	if serverID == uuid.Nil {
		return AgentInstallJob{}, &ValidationError{Msg: "server is required"}
	}
	if err := validateAgentInstallStartRequest(in); err != nil {
		return AgentInstallJob{}, err
	}
	if err := s.requireAgentInstall(ctx, actor, serverID); err != nil {
		return AgentInstallJob{}, err
	}
	job, err := s.reserveAgentInstallJob(actor.UserID, serverID)
	if err != nil {
		return AgentInstallJob{}, err
	}
	reservationOwned := true
	defer func() {
		if reservationOwned {
			s.releaseAgentInstallJob(job, true)
		}
	}()
	if s.agentInstallExists != nil {
		exists, err := s.agentInstallExists(ctx, actor, serverID)
		if err != nil {
			return AgentInstallJob{}, err
		}
		if exists {
			return AgentInstallJob{}, ErrAgentAlreadyProvisioned
		}
	} else {
		_, err := s.agentRow(ctx, actor, serverID)
		if err == nil {
			return AgentInstallJob{}, ErrAgentAlreadyProvisioned
		}
		if !errors.Is(err, store.ErrNotFound) {
			return AgentInstallJob{}, err
		}
	}
	binary, err := s.loadAgentInstallBinary()
	if err != nil {
		return AgentInstallJob{}, errors.New("trusted agent artifact unavailable")
	}
	makeCredentials := s.agentInstallCredentials
	if makeCredentials == nil {
		makeCredentials = newAgentInstallCredentials
	}
	token, certificate, privateKey, err := makeCredentials(in.Host)
	if err != nil {
		return AgentInstallJob{}, errors.New("could not prepare agent credentials")
	}
	if err := validateAgentTLSConfig(in.Host, AgentTLSConfig{SecurePort: agentInstallHTTPSPort, TrustMode: AgentTLSTrustCustom, CAPEM: certificate}); err != nil {
		return AgentInstallJob{}, errors.New("could not prepare agent TLS trust")
	}

	request := AgentInstallRequest{Host: in.Host, Port: in.Port, User: "root", Password: in.Password, ExpectedHostKey: in.HostKey, Binary: binary, Token: token, TLSListen: net.JoinHostPort("0.0.0.0", fmt.Sprint(agentInstallHTTPSPort)), TLSCertificate: certificate, TLSPrivateKey: privateKey}
	go s.runServerAgentInstall(context.Background(), actor, job, request, token, certificate)
	reservationOwned = false
	return job.read(), nil
}

func (s *Service) reserveAgentInstallJob(ownerID, serverID uuid.UUID) (*agentInstallJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentInstallJobs == nil {
		s.agentInstallJobs = make(map[uuid.UUID]*agentInstallJob)
	}
	if s.agentInstallByServer == nil {
		s.agentInstallByServer = make(map[uuid.UUID]uuid.UUID)
	}
	s.cleanupAgentInstallJobsLocked(time.Now())
	if _, exists := s.agentInstallByServer[serverID]; exists || len(s.agentInstallJobs) >= agentInstallMaxJobs {
		return nil, ErrAgentInstallBusy
	}
	id := uuid.New()
	job := &agentInstallJob{ownerID: ownerID, serverID: serverID, createdAt: time.Now(), updatedAt: time.Now(), snapshot: AgentInstallJob{ID: id, Status: "queued", Stage: "validating", Message: "Install queued"}}
	s.agentInstallJobs[id] = job
	s.agentInstallByServer[serverID] = id
	return job, nil
}

func (s *Service) releaseAgentInstallJob(job *agentInstallJob, remove bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentInstallByServer[job.serverID] == job.snapshot.ID {
		delete(s.agentInstallByServer, job.serverID)
	}
	if remove {
		delete(s.agentInstallJobs, job.snapshot.ID)
	}
}

func (s *Service) requireAgentInstall(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireAgentInstallPermissions != nil {
		return s.requireAgentInstallPermissions(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return errors.New("agent install authorization unavailable")
	}
	return s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID)
}

func (s *Service) GetServerAgentInstallJob(ctx context.Context, actor authz.Actor, serverID, jobID uuid.UUID) (AgentInstallJob, error) {
	if err := s.requireAgentInstall(ctx, actor, serverID); err != nil {
		return AgentInstallJob{}, err
	}
	s.mu.Lock()
	job := s.agentInstallJobs[jobID]
	s.mu.Unlock()
	if job == nil || job.ownerID != actor.UserID || job.serverID != serverID {
		return AgentInstallJob{}, ErrAgentInstallJobNotFound
	}
	return job.read(), nil
}

func (s *Service) cleanupAgentInstallJobsLocked(now time.Time) {
	for id, job := range s.agentInstallJobs {
		job.mu.Lock()
		terminal := job.snapshot.Status == "succeeded" || job.snapshot.Status == "failed"
		stale := now.Sub(job.updatedAt) > agentInstallJobRetention
		job.mu.Unlock()
		if terminal && stale {
			delete(s.agentInstallJobs, id)
		}
	}
	for serverID, id := range s.agentInstallByServer {
		if _, exists := s.agentInstallJobs[id]; !exists {
			delete(s.agentInstallByServer, serverID)
		}
	}
}

func (s *Service) runServerAgentInstall(parent context.Context, actor authz.Actor, job *agentInstallJob, request AgentInstallRequest, token string, caPEM []byte) {
	ctx, cancel := context.WithTimeout(parent, agentInstallJobTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			job.update("failed", "failed", "Agent installation failed; remote output was not retained")
		}
		request.Password = ""
		request.Token = ""
		request.TLSPrivateKey = nil
		request.TLSCertificate = nil
		request.Binary = nil
		s.releaseAgentInstallJob(job, false)
	}()
	job.update("running", "connecting", "Connecting to the pinned SSH host")
	run := s.agentInstallRun
	if run == nil {
		run = func(ctx context.Context, request AgentInstallRequest, update func(string)) error {
			return runAgentInstallWithProgress(ctx, request, dialAgentSSH, update)
		}
	}
	if err := run(ctx, request, func(stage string) { job.update("running", stage, installStageMessage(stage)) }); err != nil {
		job.update("failed", "failed", "Agent installation failed; remote output was not retained")
		return
	}
	job.update("running", "registering", "Registering encrypted agent credentials and TLS trust")
	register := s.agentInstallRegister
	if register == nil {
		register = s.registerInstalledAgent
	}
	if err := register(ctx, actor, job.serverID, request.Host, agentInstallHTTPSPort, token, caPEM); err != nil {
		job.update("failed", "failed", "Agent files may be installed but registration failed; inspect the target before retrying")
		return
	}
	job.update("succeeded", "complete", "Agent files installed and credentials registered; HTTPS health is not yet verified")
}

func installStageMessage(stage string) string {
	switch stage {
	case "validating":
		return "Validating agent installer"
	case "connecting":
		return "Connecting to the pinned SSH host"
	case "transferring":
		return "Transferring agent files over SFTP"
	case "activating":
		return "Activating the agent service"
	default:
		return "Installing agent"
	}
}

func (s *Service) loadAgentInstallBinary() ([]byte, error) {
	if s.agentInstallBinary != nil {
		return s.agentInstallBinary()
	}
	binary, err := readInstallArtifact("/opt/openvms/edge-agent", agentInstallMaxBinary)
	if err != nil {
		return nil, err
	}
	hash, err := readInstallArtifact("/opt/openvms/edge-agent.sha256", 128)
	if err != nil {
		return nil, err
	}
	arch, err := readInstallArtifact("/opt/openvms/edge-agent.goarch", 16)
	if err != nil {
		return nil, err
	}
	if err := verifyTrustedAgentBinary(binary, strings.TrimSpace(string(hash)), strings.TrimSpace(string(arch))); err != nil {
		return nil, err
	}
	return binary, nil
}

func readInstallArtifact(path string, maxBytes int64) ([]byte, error) {
	if maxBytes < 1 {
		return nil, errors.New("invalid artifact size bound")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("artifact is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, errors.New("trusted artifact exceeded size bound")
	}
	return data, nil
}

func verifyTrustedAgentBinary(binary []byte, expectedHash, expectedArch string) error {
	if len(binary) < 4 || len(binary) > agentInstallMaxBinary || len(expectedHash) != 64 || expectedArch != runtime.GOARCH {
		return errors.New("invalid trusted agent artifact manifest")
	}
	decoded, err := hex.DecodeString(expectedHash)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("invalid trusted agent artifact hash")
	}
	actual := sha256.Sum256(binary)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expectedHash) {
		return errors.New("trusted agent artifact hash mismatch")
	}
	file, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		return errors.New("trusted agent artifact is not ELF")
	}
	defer file.Close()
	want := elf.EM_X86_64
	if expectedArch == "arm64" {
		want = elf.EM_AARCH64
	}
	if file.Machine != want {
		return errors.New("trusted agent artifact architecture mismatch")
	}
	return nil
}

func newAgentInstallCredentials(host string) (token string, certificate, privateKey []byte, err error) {
	if net.ParseIP(host) == nil || net.ParseIP(host).To4() == nil {
		return "", nil, nil, errors.New("invalid agent IPv4")
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		return "", nil, nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(tokenBytes)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", nil, nil, err
	}
	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", nil, nil, err
	}
	ip := net.ParseIP(host).To4()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "OpenVMS edge agent"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{ip}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", nil, nil, err
	}
	certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKey = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return token, certificate, privateKey, nil
}

func (s *Service) registerInstalledAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID, host string, securePort uint16, token string, caPEM []byte) error {
	if err := s.requireAgentInstall(ctx, actor, serverID); err != nil {
		return err
	}
	if s.Store == nil || s.Sealer == nil {
		return errors.New("agent installation persistence unavailable")
	}
	if err := validateAgentTLSConfig(host, AgentTLSConfig{SecurePort: uint32(securePort), TrustMode: AgentTLSTrustCustom, CAPEM: caPEM}); err != nil {
		return err
	}
	sealed, err := s.Sealer.Seal([]byte(token), serverID[:])
	if err != nil {
		return errors.New("could not protect agent credential")
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
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
		rows, err := q.RegisterServerAgentIfAbsent(ctx, db.RegisterServerAgentIfAbsentParams{ServerID: serverID, TenantID: server.TenantID, Host: host, Port: 7419, Variant: "ssh", TokenSealed: sealed, Version: agent.Version})
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrAgentAlreadyProvisioned
		}
		ca := string(caPEM)
		rows, err = q.RegisterServerAgentTLSIfAbsent(ctx, db.RegisterServerAgentTLSIfAbsentParams{ServerID: serverID, TenantID: server.TenantID, SecurePort: int32(securePort), TrustMode: AgentTLSTrustCustom, CaPem: &ca})
		if err != nil {
			return err
		}
		if rows == 0 {
			return errors.New("agent TLS metadata already exists")
		}
		fingerprint := sha256.Sum256(caPEM)
		details, err := json.Marshal(map[string]any{"host": host, "http_port": 7419, "https_port": securePort, "agent_ca_sha256": hex.EncodeToString(fingerprint[:])})
		if err != nil {
			return err
		}
		return q.InsertAudit(ctx, db.InsertAuditParams{TenantID: &server.TenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: "SERVER_AGENT_SSH_INSTALLED", TargetType: "server_agent", TargetID: &serverID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: details})
	})
}

func runAgentInstallWithProgress(ctx context.Context, request AgentInstallRequest, dial AgentInstallDialer, progress func(string)) error {
	if progress != nil {
		progress("validating")
	}
	if err := validateAgentInstallRequest(request); err != nil {
		return err
	}
	if dial == nil {
		return errInvalidAgentInstall
	}
	if ctx.Err() != nil {
		return errors.New("agent install cancelled")
	}
	installCtx, cancel := context.WithTimeout(ctx, agentInstallDeadline)
	defer cancel()
	if progress != nil {
		progress("connecting")
	}
	conn, err := dial(installCtx, request.Host, request.Port, request.User, request.Password, request.ExpectedHostKey)
	if err != nil {
		return errors.New("agent install SSH connection failed")
	}
	defer conn.Close()
	preflight, err := runAgentInstallStep(installCtx, conn, agentInstallPreflight())
	if err != nil || !validAgentInstallPreflight(preflight) {
		return errors.New("agent install preflight failed")
	}
	files, err := agentInstallFiles(request)
	if err != nil {
		return errors.New("agent install payload is invalid")
	}
	if progress != nil {
		progress("transferring")
	}
	for _, file := range files {
		if installCtx.Err() != nil {
			return errors.New("agent install deadline exceeded")
		}
		fileCtx, fileCancel := context.WithTimeout(installCtx, 2*time.Minute)
		err = conn.WriteSFTPFile(fileCtx, file.path, file.mode, file.data)
		fileCancel()
		if err != nil {
			return errors.New("agent install file transfer failed")
		}
	}
	if progress != nil {
		progress("activating")
	}
	activation, err := runAgentInstallStep(installCtx, conn, agentInstallActivate)
	if err != nil || strings.TrimSpace(activation) != "openvms_agent_install=active" {
		return errors.New("agent install activation failed")
	}
	return nil
}
