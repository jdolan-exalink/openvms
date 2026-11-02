package provision

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func urlErr(addr string, cause error) error {
	return &url.Error{Op: "Get", URL: "http://" + addr + "/v1/metrics", Err: cause}
}

func withProbe(t *testing.T, probe func(string) error) {
	t.Helper()
	prev := agentDiagDial
	agentDiagDial = func(_ context.Context, address string) error { return probe(address) }
	t.Cleanup(func() { agentDiagDial = prev })
}

func TestDiagnoseAgentErrorNamesTheCauseAndTheAction(t *testing.T) {
	const addr = "192.168.70.162:7419"
	tests := []struct {
		name  string
		err   error
		probe func(string) error
		want  []string
	}{
		{"timeout while the port accepts the connection", urlErr(addr, context.DeadlineExceeded), func(string) error { return nil },
			[]string{"acepta la conexión pero no responde a tiempo", "journalctl -u openvms-agent", addr}},
		{"timeout and the network does not answer either", urlErr(addr, context.DeadlineExceeded), func(string) error { return context.DeadlineExceeded },
			[]string{"firewall", "nc -vz 192.168.70.162 7419"}},
		{"timeout but the port is closed", urlErr(addr, context.DeadlineExceeded), func(string) error { return syscall.ECONNREFUSED },
			[]string{"rechazó la conexión", "systemctl status openvms-agent"}},
		{"connection refused", urlErr(addr, syscall.ECONNREFUSED), nil, []string{"rechazó la conexión", "systemctl status openvms-agent"}},
		{"no route", urlErr(addr, syscall.EHOSTUNREACH), nil, []string{"No hay ruta hasta " + addr, "VPN"}},
		{"network unreachable", urlErr(addr, syscall.ENETUNREACH), nil, []string{"No hay ruta"}},
		{"name does not resolve", urlErr("agente.local:7419", &net.DNSError{Name: "agente.local", Err: "no such host"}), nil, []string{"No se pudo resolver el nombre agente.local"}},
		{"unknown certificate authority", urlErr(addr, x509.UnknownAuthorityError{}), nil, []string{"Falló el TLS", "certificado"}},
		{"net timeout error", urlErr(addr, &net.OpError{Op: "read", Err: timeoutErr{}}), func(string) error { return nil }, []string{"acepta la conexión"}},
		{"something else keeps its text", errors.New("boom"), nil, []string{"boom"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.probe != nil {
				withProbe(t, tc.probe)
			}
			got := diagnoseAgentError(context.Background(), tc.err)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("diagnosis %q is missing %q", got, want)
				}
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestDiagnoseAgentErrorWithoutAddressDoesNotProbe(t *testing.T) {
	withProbe(t, func(string) error { t.Fatal("must not probe an unknown address"); return nil })
	if got := diagnoseAgentError(context.Background(), context.DeadlineExceeded); !strings.Contains(got, "no respondió a tiempo") {
		t.Fatalf("got %q", got)
	}
	if diagnoseAgentError(context.Background(), nil) != "" {
		t.Fatal("no error, no diagnosis")
	}
}

func TestAgentUnreachableErrorStillMatchesTheSentinelAndCarriesTheReason(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &AgentUnreachableError{Detail: "firewall?"})
	if !errors.Is(err, ErrAgentConnectUnreachable) {
		t.Fatal("callers match the sentinel with errors.Is")
	}
	var ue *AgentUnreachableError
	if !errors.As(err, &ue) || !strings.Contains(ue.Error(), "firewall?") {
		t.Fatalf("detail lost: %v", err)
	}
}
