package provision

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const agentInstallTestPin = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type fakeAgentInstallConn struct {
	commands        []string
	files           map[string][]byte
	modes           map[string]os.FileMode
	runErrAt        string
	runErr          error
	preflightOutput string
	writeErr        error
	writes          int
}

func (f *fakeAgentInstallConn) Run(_ context.Context, command string) (string, error) {
	f.commands = append(f.commands, command)
	if f.runErrAt != "" && strings.Contains(command, f.runErrAt) {
		return "remote leaked detail", f.runErr
	}
	if strings.Contains(command, "openvms_install_preflight") {
		if f.preflightOutput != "" {
			return f.preflightOutput, nil
		}
		return "openvms_install_preflight=ok os=ubuntu arch=" + runtime.GOARCH + " user=root targets=absent\n", nil
	}
	if strings.Contains(command, "openvms_agent_install=active") {
		return "openvms_agent_install=active\n", nil
	}
	return "active", nil
}

func (f *fakeAgentInstallConn) WriteSFTPFile(_ context.Context, path string, mode os.FileMode, content []byte) error {
	f.writes++
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.files == nil {
		f.files = map[string][]byte{}
		f.modes = map[string]os.FileMode{}
	}
	if _, exists := f.files[path]; exists {
		return errors.New("file already exists")
	}
	f.files[path] = append([]byte(nil), content...)
	f.modes[path] = mode
	return nil
}

func (f *fakeAgentInstallConn) Close() error { return nil }

func newAgentInstallTLS(t *testing.T, ip string) ([]byte, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: ip},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP(ip)},
	}, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: ip},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP(ip)},
	}, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func validAgentInstallRequest(t *testing.T) AgentInstallRequest {
	t.Helper()
	cert, key := newAgentInstallTLS(t, "192.0.2.10")
	return AgentInstallRequest{
		Host: "192.0.2.10", Port: 2202, User: "root", Password: "ssh-password-never-persist",
		ExpectedHostKey: agentInstallTestPin, Binary: []byte("agent-binary"), Token: "agent-token-1234567890123456789012345678901234567890",
		TLSListen: "0.0.0.0:7443", TLSCertificate: cert, TLSPrivateKey: key,
	}
}

func TestAgentInstallRequestSerializationRedactsSecrets(t *testing.T) {
	request := validAgentInstallRequest(t)
	representations := []string{fmt.Sprint(request), fmt.Sprintf("%+v", request), fmt.Sprintf("%#v", request)}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	representations = append(representations, string(encoded))
	for _, secret := range []string{request.Password, request.Token, string(request.TLSPrivateKey), string(request.Binary), string(request.TLSCertificate)} {
		if secret != "" {
			for _, representation := range representations {
				if strings.Contains(representation, secret) {
					t.Fatalf("serialized request leaked secret or payload: %q", representation)
				}
			}
		}
	}
	if !strings.Contains(representations[0], request.Host) || !strings.Contains(string(encoded), request.Host) {
		t.Fatal("safe request metadata should remain inspectable")
	}
}

func TestRunAgentInstallValidatesAllInputsBeforeDial(t *testing.T) {
	base := validAgentInstallRequest(t)
	tests := []struct {
		name   string
		mutate func(*AgentInstallRequest)
	}{
		{"DNS is not allowed", func(r *AgentInstallRequest) { r.Host = "agent.example.test" }},
		{"noncanonical IPv4 is not allowed", func(r *AgentInstallRequest) { r.Host = "192.000.002.010" }},
		{"invalid port", func(r *AgentInstallRequest) { r.Port = 0 }},
		{"non-root account", func(r *AgentInstallRequest) { r.User = "operator" }},
		{"empty password", func(r *AgentInstallRequest) { r.Password = "" }},
		{"missing pin", func(r *AgentInstallRequest) { r.ExpectedHostKey = "" }},
		{"bad pin", func(r *AgentInstallRequest) { r.ExpectedHostKey = "SHA256:bad" }},
		{"empty binary", func(r *AgentInstallRequest) { r.Binary = nil }},
		{"empty token", func(r *AgentInstallRequest) { r.Token = "" }},
		{"partial TLS settings", func(r *AgentInstallRequest) { r.TLSPrivateKey = nil }},
		{"TLS listener wrong address", func(r *AgentInstallRequest) { r.TLSListen = "192.0.2.11:7443" }},
		{"TLS listener invalid port", func(r *AgentInstallRequest) { r.TLSListen = "0.0.0.0:7419" }},
		{"TLS certificate wrong SAN", func(r *AgentInstallRequest) { r.TLSCertificate, r.TLSPrivateKey = newAgentInstallTLS(t, "192.0.2.11") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := base
			tt.mutate(&request)
			dials := 0
			err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
				dials++
				return &fakeAgentInstallConn{}, nil
			})
			if err == nil || !strings.Contains(err.Error(), "invalid agent install request") {
				t.Fatalf("error = %v", err)
			}
			if request.Password != "" && strings.Contains(err.Error(), request.Password) || dials != 0 {
				t.Fatalf("invalid input reached SSH or leaked a credential: err=%q dials=%d", err, dials)
			}
		})
	}
}

func TestRunAgentInstallUsesPinnedRootAndOnlyAgentTargets(t *testing.T) {
	request := validAgentInstallRequest(t)
	conn := &fakeAgentInstallConn{}
	var dialArgs []string
	err := RunAgentInstall(context.Background(), request, func(_ context.Context, host string, port uint16, user, password, pin string) (AgentInstallConn, error) {
		dialArgs = []string{host, fmt.Sprint(port), user, password, pin}
		return conn, nil
	})
	if err != nil {
		t.Fatalf("RunAgentInstall: %v", err)
	}
	if !reflect.DeepEqual(dialArgs, []string{request.Host, "2202", "root", request.Password, request.ExpectedHostKey}) {
		t.Fatalf("dial args %#v", dialArgs)
	}
	wantModes := map[string]os.FileMode{
		"/usr/local/bin/openvms-agent":              0o755,
		"/etc/openvms/agent.token":                  0o600,
		"/etc/openvms/agent.env":                    0o600,
		"/etc/openvms/agent.crt":                    0o600,
		"/etc/openvms/agent.key":                    0o600,
		"/etc/systemd/system/openvms-agent.service": 0o644,
	}
	if !reflect.DeepEqual(conn.modes, wantModes) {
		t.Fatalf("uploaded file modes %#v", conn.modes)
	}
	if string(conn.files["/etc/openvms/agent.token"]) != request.Token {
		t.Fatal("agent token was not transferred as an independent file payload")
	}
	commands := strings.Join(conn.commands, "\n")
	for _, forbidden := range []string{request.Password, request.Token, string(request.TLSPrivateKey), "apt", "docker", "compose", "chrony", "ntp", "/opt/frigate"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("remote command contains forbidden secret or scope: %q", forbidden)
		}
	}
	for _, want := range []string{"OPENVMS_AGENT_LISTEN=0.0.0.0:7419", "OPENVMS_AGENT_ONVIF_TLS_LISTEN=0.0.0.0:7443", "TLS_CERT_FILE=/etc/openvms/agent.crt", "TLS_KEY_FILE=/etc/openvms/agent.key"} {
		if !strings.Contains(string(conn.files["/etc/openvms/agent.env"]), want) {
			t.Fatalf("agent environment lacks %q: %q", want, conn.files["/etc/openvms/agent.env"])
		}
	}
}

func TestRunAgentInstallWithoutTLSDoesNotClaimHTTPSReadiness(t *testing.T) {
	request := validAgentInstallRequest(t)
	request.TLSListen, request.TLSCertificate, request.TLSPrivateKey = "", nil, nil
	conn := &fakeAgentInstallConn{}
	err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return conn, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	env := string(conn.files["/etc/openvms/agent.env"])
	if strings.Contains(env, "TLS_CERT_FILE") || strings.Contains(env, "OPENVMS_AGENT_ONVIF_TLS_LISTEN") {
		t.Fatalf("TLS was partially enabled: %q", env)
	}
}

func TestRunAgentInstallRedactsDialAndRemoteErrorsAndRefusesOverwrite(t *testing.T) {
	request := validAgentInstallRequest(t)
	const secret = "ssh-password-never-persist"
	if err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return nil, fmt.Errorf("remote leaked %s", secret)
	}); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("dial error was exposed: %v", err)
	}
	conn := &fakeAgentInstallConn{runErrAt: "openvms_install_preflight", runErr: fmt.Errorf("remote leaked %s", secret)}
	if err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return conn, nil
	}); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("remote error was exposed: %v", err)
	}
	if len(conn.files) != 0 {
		t.Fatalf("files were uploaded despite preflight failure: %#v", conn.files)
	}
}

func TestRunAgentInstallStopsOnUnsupportedHostAndTransferFailure(t *testing.T) {
	request := validAgentInstallRequest(t)
	unsupported := &fakeAgentInstallConn{preflightOutput: "openvms_install_preflight=ok os=ubuntu arch=386 user=root targets=absent"}
	if err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return unsupported, nil
	}); err == nil {
		t.Fatal("unsupported host was accepted")
	}
	if unsupported.writes != 0 {
		t.Fatalf("unsupported host received %d payloads", unsupported.writes)
	}
	failed := &fakeAgentInstallConn{writeErr: errors.New("remote SFTP error with secret")}
	err := RunAgentInstall(context.Background(), request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		return failed, nil
	})
	if err == nil || strings.Contains(err.Error(), request.Password) || strings.Contains(err.Error(), request.Token) {
		t.Fatalf("transfer failure was not redacted: %v", err)
	}
	if len(failed.commands) != 1 || failed.writes != 1 {
		t.Fatalf("transfer failure ran later commands: commands=%d writes=%d", len(failed.commands), failed.writes)
	}
}

func TestRunAgentInstallHonorsCancellationBeforeDial(t *testing.T) {
	request := validAgentInstallRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dials := 0
	err := RunAgentInstall(ctx, request, func(context.Context, string, uint16, string, string, string) (AgentInstallConn, error) {
		dials++
		return &fakeAgentInstallConn{}, nil
	})
	if err == nil || dials != 0 {
		t.Fatalf("cancelled install reached dial: err=%v dials=%d", err, dials)
	}
}

func TestPinnedHostCallbackRejectsMismatchedKeyWithoutLearningIt(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	callback := clientConfig("root", "password", agentInstallTestPin, func(string) { seen = true }).HostKeyCallback
	err = callback("192.0.2.10", nil, signer.PublicKey())
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") || seen {
		t.Fatalf("mismatched pin callback error=%v onKeyCalled=%v", err, seen)
	}
}
