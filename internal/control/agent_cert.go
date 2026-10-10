package control

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// maxRenewCSRBytes bounds a renewal CSR; a P-256 CSR is a few hundred bytes and the enroll
// endpoint caps its PEM at the same size.
const maxRenewCSRBytes = 8192

var errCSRSize = status.Errorf(codes.InvalidArgument, "csr_pem is required and must be at most %d bytes", maxRenewCSRBytes)

// CertificateRenewer issues a replacement certificate for an authenticated agent
// (agentenroll.Service). previousSerial is the certificate the agent authenticated with.
type CertificateRenewer interface {
	Renew(ctx context.Context, id agentca.Identity, previousSerial string, csrPEM []byte) (*agentca.Issuance, error)
}

// AgentCertServer is the AgentService on the agent listener. Embedding the unimplemented
// server keeps future methods unreachable until they are written.
type AgentCertServer struct {
	openvmsv1.UnimplementedAgentServiceServer
	Renewer CertificateRenewer
	Log     *slog.Logger
}

// RenewCertificate signs the CSR for the caller's own identity. That identity comes only from
// the client certificate the interceptors authenticated; the request carries nothing that
// names an agent, and the CSR's subject and SANs are ignored by the CA.
func (s *AgentCertServer) RenewCertificate(ctx context.Context, req *openvmsv1.RenewCertificateRequest) (*openvmsv1.RenewCertificateResponse, error) {
	id, ok := agentauth.IdentityFromContext(ctx)
	serial, hasSerial := agentauth.SerialFromContext(ctx)
	if !ok || !hasSerial {
		return nil, agentauth.ErrRejected
	}
	if len(req.CsrPem) == 0 || len(req.CsrPem) > maxRenewCSRBytes {
		return nil, errCSRSize
	}
	res, err := s.Renewer.Renew(ctx, id, serial, []byte(req.CsrPem))
	if errors.Is(err, agentca.ErrInvalidCSR) {
		return nil, status.Error(codes.InvalidArgument, "invalid certificate request")
	}
	if errors.Is(err, agentca.ErrNotRenewable) {
		// The presenting certificate was revoked or superseded between the listener's check and
		// the renewal: the same refusal as any invalid certificate, not a server fault.
		s.log().Warn("agent certificate renewal refused", "server_id", id.ServerID, "serial", serial, "error", err)
		return nil, agentauth.ErrRejected
	}
	if errors.Is(err, agentca.ErrRenewalTooEarly) {
		return nil, status.Error(codes.FailedPrecondition, "certificate renewal not allowed yet")
	}
	if err != nil {
		s.log().Error("agent certificate renewal failed", "server_id", id.ServerID, "serial", serial, "error", err)
		return nil, status.Error(codes.Internal, "certificate renewal failed")
	}
	return &openvmsv1.RenewCertificateResponse{CertificatePem: string(res.Issued.CertPEM), CaPem: string(res.CAPEM)}, nil
}

func (s *AgentCertServer) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}
