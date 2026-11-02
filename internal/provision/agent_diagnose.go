package provision

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// agentDiagDial opens the short TCP probe that tells "nothing answers on the network" from "the port
// accepts the connection but the agent does not reply". Tests replace it.
var agentDiagDial = func(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

// AgentUnreachableError is ErrAgentConnectUnreachable with the reason, so the operator is told what to
// check instead of a bare "could not reach".
type AgentUnreachableError struct{ Detail string }

func (e *AgentUnreachableError) Error() string {
	return ErrAgentConnectUnreachable.Error() + ": " + e.Detail
}

func (e *AgentUnreachableError) Is(target error) bool { return target == ErrAgentConnectUnreachable }

// diagnoseAgentError turns a failure to reach an agent into what an operator can act on: a firewall that
// drops the traffic, a closed port, a missing route, a name that does not resolve, a TLS mismatch, or an
// agent that accepts the connection and never answers (a blocked agent or an overloaded host). The
// original error text is kept when none of those fits.
func diagnoseAgentError(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	address := agentErrorAddress(err)
	where := address
	if where == "" {
		where = "el agente"
	}
	var dns *net.DNSError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var certInvalid x509.CertificateInvalidError
	var tlsRecord tls.RecordHeaderError
	switch {
	case errors.As(err, &dns):
		return fmt.Sprintf("No se pudo resolver el nombre %s: revisá el DNS o registrá el agente con su IP.", dns.Name)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Sprintf("%s rechazó la conexión: el agente no está corriendo o escucha en otro puerto. En el servidor: systemctl status openvms-agent.", where)
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return fmt.Sprintf("No hay ruta hasta %s: revisá la red, la VPN o el gateway entre OpenVMS y el servidor.", where)
	case errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &certInvalid), errors.As(err, &tlsRecord), strings.Contains(err.Error(), "tls:"):
		return fmt.Sprintf("Falló el TLS con %s: el certificado no es el esperado o el agente no usa TLS en ese puerto (%v).", where, err)
	case isTimeout(err):
		if address == "" {
			return "El agente no respondió a tiempo."
		}
		switch probe := agentDiagDial(ctx, address); {
		case probe == nil:
			return fmt.Sprintf("%s acepta la conexión pero no responde a tiempo: el agente está bloqueado o el servidor sobrecargado. En el servidor: journalctl -u openvms-agent, uptime y free -m.", address)
		case errors.Is(probe, syscall.ECONNREFUSED):
			return fmt.Sprintf("%s rechazó la conexión: el agente no está corriendo o escucha en otro puerto. En el servidor: systemctl status openvms-agent.", address)
		default:
			return fmt.Sprintf("%s no responde a nivel de red (sin respuesta en 2 s): lo más común es un firewall que descarta el tráfico a ese puerto, una ruta o VPN caída, o una IP equivocada. Probá desde el servidor de OpenVMS: nc -vz %s.", address, strings.Replace(address, ":", " ", 1))
		}
	}
	return err.Error()
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// agentErrorAddress is host:port of the request that failed, when the error names it.
func agentErrorAddress(err error) string {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return ""
	}
	u, perr := url.Parse(ue.URL)
	if perr != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}
