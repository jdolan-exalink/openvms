package provision

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func validAgentInstallStartRequest() AgentInstallStartRequest {
	return AgentInstallStartRequest{Host: "192.0.2.44", Port: 2202, Password: "never-persist-ssh-password", HostKey: agentInstallTestPin}
}

func TestStartAgentInstallDeniesBeforeRegistrationLookupArtifactOrRunner(t *testing.T) {
	var mu sync.Mutex
	var order []string
	appendOrder := func(v string) { mu.Lock(); defer mu.Unlock(); order = append(order, v) }
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error {
			appendOrder("permissions")
			return errors.New("denied")
		},
		agentInstallExists: func(context.Context, authz.Actor, uuid.UUID) (bool, error) {
			appendOrder("agent-row")
			return false, nil
		},
		agentInstallBinary: func() ([]byte, error) { appendOrder("binary"); return []byte("fake"), nil },
		agentInstallRun:    func(context.Context, AgentInstallRequest, func(string)) error { appendOrder("ssh"); return nil },
	}
	_, err := svc.StartServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, uuid.New(), validAgentInstallStartRequest())
	if err == nil || err.Error() != "denied" {
		t.Fatalf("StartServerAgentInstall error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"permissions"}) {
		t.Fatalf("access order = %#v", order)
	}
}

func TestStartAgentInstallExistingAgentRefusedBeforeArtifactOrRunner(t *testing.T) {
	var order []string
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { order = append(order, "permissions"); return nil },
		agentInstallExists: func(context.Context, authz.Actor, uuid.UUID) (bool, error) {
			order = append(order, "agent-row")
			return true, nil
		},
		agentInstallBinary: func() ([]byte, error) { order = append(order, "binary"); return []byte("fake"), nil },
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error {
			order = append(order, "ssh")
			return nil
		},
	}
	_, err := svc.StartServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, uuid.New(), validAgentInstallStartRequest())
	if err != ErrAgentAlreadyProvisioned {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"permissions", "agent-row"}) {
		t.Fatalf("access order = %#v", order)
	}
}

func TestAgentInstallJobDoesNotRetainOrSerializeCredentials(t *testing.T) {
	password := "never-persist-ssh-password"
	job := AgentInstallJob{ID: uuid.New(), Status: "running", Stage: "connecting", Message: "Connecting"}
	encoded, err := json.Marshal(struct {
		Job      AgentInstallJob `json:"job"`
		Password string          `json:"-"`
	}{Job: job, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{string(encoded), fmt.Sprint(job), fmt.Sprintf("%+v", job), fmt.Sprintf("%#v", job)} {
		if strings.Contains(rendered, password) {
			t.Fatalf("job projection leaked password: %s", rendered)
		}
	}
}

func waitAgentInstallJob(t *testing.T, svc *Service, actor authz.Actor, serverID uuid.UUID, job AgentInstallJob) AgentInstallJob {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, err := svc.GetServerAgentInstallJob(context.Background(), actor, serverID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "succeeded" || current.Status == "failed" {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("agent install job did not finish")
	return AgentInstallJob{}
}

func TestStartAgentInstallRegistersOnlyAfterRunnerSuccess(t *testing.T) {
	actor := authz.Actor{UserID: uuid.New()}
	serverID := uuid.New()
	const password = "never-persist-ssh-password"
	var registered bool
	var registeredToken string
	var registeredCA []byte
	var requested AgentInstallRequest
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("trusted fake binary"), nil },
		agentInstallRun: func(_ context.Context, in AgentInstallRequest, update func(string)) error {
			requested = in
			update("transferring")
			update("activating")
			return nil
		},
		agentInstallRegister: func(_ context.Context, _ authz.Actor, _ uuid.UUID, host string, port uint16, token string, ca []byte) error {
			if host != "192.0.2.44" || port != agentInstallHTTPSPort {
				t.Fatalf("registration target=%s:%d", host, port)
			}
			registered, registeredToken, registeredCA = true, token, append([]byte(nil), ca...)
			return nil
		},
	}
	request := validAgentInstallStartRequest()
	request.Password = password
	job, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, request)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "queued" {
		t.Fatalf("initial job = %#v", job)
	}
	final := waitAgentInstallJob(t, svc, actor, serverID, job)
	if final.Status != "succeeded" || final.Stage != "complete" {
		t.Fatalf("final job = %#v", final)
	}
	if !registered || registeredToken == "" || len(registeredCA) == 0 {
		t.Fatal("agent credentials/trust were not registered")
	}
	if requested.Password != password || requested.User != "root" || requested.ExpectedHostKey != request.HostKey {
		t.Fatal("runner did not receive exact pinned root credentials")
	}
	if got := requested.String(); strings.Contains(got, password) {
		t.Fatalf("runner request leaked password: %q", got)
	}
	if got, _ := json.Marshal(final); strings.Contains(string(got), registeredToken) || strings.Contains(string(got), string(registeredCA)) {
		t.Fatalf("job response leaked credentials: %s", got)
	}
}

func TestStartAgentInstallRunnerFailureDoesNotRegisterOrEchoRemoteOutput(t *testing.T) {
	actor := authz.Actor{UserID: uuid.New()}
	serverID := uuid.New()
	const password = "never-persist-ssh-password"
	registered := false
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("trusted fake binary"), nil },
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error {
			return errors.New("remote leaked " + password)
		},
		agentInstallRegister: func(context.Context, authz.Actor, uuid.UUID, string, uint16, string, []byte) error {
			registered = true
			return nil
		},
	}
	request := validAgentInstallStartRequest()
	request.Password = password
	job, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, request)
	if err != nil {
		t.Fatal(err)
	}
	final := waitAgentInstallJob(t, svc, actor, serverID, job)
	if final.Status != "failed" || registered {
		t.Fatalf("job=%#v registered=%v", final, registered)
	}
	if strings.Contains(final.Message, password) || strings.Contains(final.Message, "remote leaked") {
		t.Fatalf("job leaked remote error: %#v", final)
	}
}

type installContextSecretKey struct{}

func TestStartAgentInstallDoesNotCarryRequestContextIntoWorker(t *testing.T) {
	actor, serverID := authz.Actor{UserID: uuid.New()}, uuid.New()
	secret := "raw-browser-session-secret"
	var gotContext context.Context
	started := make(chan struct{})
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary:             func() ([]byte, error) { return []byte("fake"), nil },
		agentInstallRun: func(ctx context.Context, _ AgentInstallRequest, _ func(string)) error {
			gotContext = ctx
			close(started)
			return errors.New("stop fake run")
		},
	}
	requestContext := context.WithValue(context.Background(), installContextSecretKey{}, secret)
	if _, err := svc.StartServerAgentInstall(requestContext, actor, serverID, validAgentInstallStartRequest()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if gotContext == nil || gotContext.Value(installContextSecretKey{}) != nil {
		t.Fatal("worker retained a value from the request context")
	}
	if _, ok := gotContext.Deadline(); !ok {
		t.Fatal("worker context is not bounded by a deadline")
	}
}

func TestAgentInstallCapacityIsReservedBeforeArtifactOrCredentialPreparation(t *testing.T) {
	svc := &Service{agentInstallJobs: make(map[uuid.UUID]*agentInstallJob), agentInstallByServer: make(map[uuid.UUID]uuid.UUID)}
	for i := 0; i < agentInstallMaxJobs; i++ {
		id := uuid.New()
		serverID := uuid.New()
		svc.agentInstallJobs[id] = &agentInstallJob{serverID: serverID, updatedAt: time.Now(), snapshot: AgentInstallJob{Status: "running"}}
		svc.agentInstallByServer[serverID] = id
	}
	artifactReads, credentialGenerations := 0, 0
	svc.requireAgentInstallPermissions = func(context.Context, authz.Actor, uuid.UUID) error { return nil }
	svc.agentInstallExists = func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil }
	svc.agentInstallBinary = func() ([]byte, error) { artifactReads++; return []byte("fake"), nil }
	svc.agentInstallCredentials = func(string) (string, []byte, []byte, error) {
		credentialGenerations++
		return "token", []byte("cert"), []byte("key"), nil
	}
	if _, err := svc.StartServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, uuid.New(), validAgentInstallStartRequest()); err != ErrAgentInstallBusy {
		t.Fatalf("capacity error = %v", err)
	}
	if artifactReads != 0 || credentialGenerations != 0 {
		t.Fatalf("capacity rejection did expensive preparation: artifact reads=%d credential generations=%d", artifactReads, credentialGenerations)
	}
}

func TestAgentInstallDuplicateServerIsRejectedBeforeArtifactOrCredentialPreparation(t *testing.T) {
	serverID := uuid.New()
	activeID := uuid.New()
	svc := &Service{
		agentInstallJobs:     map[uuid.UUID]*agentInstallJob{activeID: {serverID: serverID, updatedAt: time.Now(), snapshot: AgentInstallJob{Status: "running"}}},
		agentInstallByServer: map[uuid.UUID]uuid.UUID{serverID: activeID},
		agentInstallExists:   func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
	}
	artifactReads, credentialGenerations := 0, 0
	svc.requireAgentInstallPermissions = func(context.Context, authz.Actor, uuid.UUID) error { return nil }
	svc.agentInstallBinary = func() ([]byte, error) { artifactReads++; return []byte("fake"), nil }
	svc.agentInstallCredentials = func(string) (string, []byte, []byte, error) {
		credentialGenerations++
		return "token", []byte("cert"), []byte("key"), nil
	}
	if _, err := svc.StartServerAgentInstall(context.Background(), authz.Actor{UserID: uuid.New()}, serverID, validAgentInstallStartRequest()); err != ErrAgentInstallBusy {
		t.Fatalf("duplicate error = %v", err)
	}
	if artifactReads != 0 || credentialGenerations != 0 {
		t.Fatalf("duplicate rejection did expensive preparation: artifact reads=%d credential generations=%d", artifactReads, credentialGenerations)
	}
}

func TestAgentInstallPreparationFailureReleasesReservation(t *testing.T) {
	serverID := uuid.New()
	artifactReads := 0
	svc := &Service{
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
		agentInstallExists:             func(context.Context, authz.Actor, uuid.UUID) (bool, error) { return false, nil },
		agentInstallBinary: func() ([]byte, error) {
			artifactReads++
			if artifactReads == 1 {
				return nil, errors.New("artifact unavailable")
			}
			return []byte("fake"), nil
		},
		agentInstallRun: func(context.Context, AgentInstallRequest, func(string)) error { return errors.New("stop fake run") },
	}
	actor := authz.Actor{UserID: uuid.New()}
	if _, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, validAgentInstallStartRequest()); err == nil {
		t.Fatal("expected preparation failure")
	}
	if len(svc.agentInstallJobs) != 0 || len(svc.agentInstallByServer) != 0 {
		t.Fatal("preparation failure leaked the reservation")
	}
	if _, err := svc.StartServerAgentInstall(context.Background(), actor, serverID, validAgentInstallStartRequest()); err != nil {
		t.Fatalf("reservation remained busy after preparation failure: %v", err)
	}
}

func TestReadInstallArtifactEnforcesBoundAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/artifact"
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstallArtifact(path, 4); err == nil {
		t.Fatal("oversized artifact accepted")
	}
	link := dir + "/link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstallArtifact(link, 8); err == nil {
		t.Fatal("symlink artifact accepted")
	}
}

func TestTrustedAgentArtifactRequiresManifestHashAndMatchingArchitecture(t *testing.T) {
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	hash := hex.EncodeToString(sum[:])
	if err := verifyTrustedAgentBinary(binary, hash, runtime.GOARCH); err != nil {
		t.Fatalf("valid test ELF rejected: %v", err)
	}
	if err := verifyTrustedAgentBinary(binary, strings.Repeat("0", 64), runtime.GOARCH); err == nil {
		t.Fatal("wrong hash accepted")
	}
	wrongArch := "arm64"
	if runtime.GOARCH == "arm64" {
		wrongArch = "amd64"
	}
	if err := verifyTrustedAgentBinary(binary, hash, wrongArch); err == nil {
		t.Fatal("wrong architecture accepted")
	}
}

func TestGeneratedAgentCredentialsHaveMatchingIPCertificateAndBoundedToken(t *testing.T) {
	const host = "192.0.2.44"
	token, certPEM, keyPEM, err := newAgentInstallCredentials(host)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("generated token length=%d err=%v", len(decoded), err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("generated pair invalid: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("generated public CA did not parse")
	}
	block, _ := pem.Decode(certPEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("generated certificate does not verify for the fixed IP: %v", err)
	}
	if err := validateAgentTLSConfig(host, AgentTLSConfig{SecurePort: agentInstallHTTPSPort, TrustMode: AgentTLSTrustCustom, CAPEM: certPEM}); err != nil {
		t.Fatalf("generated public trust config invalid: %v", err)
	}
}
