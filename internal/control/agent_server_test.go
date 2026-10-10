package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

type agentHarness struct {
	addr     string
	serverCA string // PEM file of the (self-signed) server certificate, trusted by clients
	pki      *agentauthtest.PKI
}

func startAgentServer(t *testing.T) *agentHarness {
	t.Helper()
	pki := agentauthtest.NewPKI(t)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pki.Pool)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewAgentServer(AgentConfig{Credentials: creds, Verifier: &agentauth.Verifier{Certs: pki.Store}})
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.GRPCServer().Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &agentHarness{addr: lis.Addr().String(), serverCA: certFile, pki: pki}
}

// dial connects with the given client certificate (none when nil), trusting the server cert.
func (h *agentHarness) dial(t *testing.T, client *tls.Certificate) openvmsv1.NodeServiceClient {
	t.Helper()
	var creds credentials.TransportCredentials
	if client != nil {
		// ClientCredentials has no client-certificate option (agents get one in a later
		// task), so the test builds the client TLS config itself.
		creds = clientTLS(t, h.serverCA, *client)
	} else {
		var err error
		if creds, err = grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: h.serverCA}); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return openvmsv1.NewNodeServiceClient(conn)
}

func clientTLS(t *testing.T, serverCAFile string, cert tls.Certificate) credentials.TransportCredentials {
	t.Helper()
	pemBytes, err := os.ReadFile(serverCAFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("server certificate is not PEM")
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, Certificates: []tls.Certificate{cert}})
}

func heartbeat(c openvmsv1.NodeServiceClient, nodeID string) (*openvmsv1.NodeHeartbeatResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Heartbeat(ctx, &openvmsv1.NodeHeartbeatRequest{NodeId: nodeID})
}

var agentID = agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}

func TestAgentListener_ValidCertHeartbeats(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	resp, err := heartbeat(h.dial(t, &c.TLS), agentID.ServerID.String())
	if err != nil || !resp.Acknowledged {
		t.Fatalf("Heartbeat = %+v, %v", resp, err)
	}
}

func TestAgentListener_RejectsWithoutAValidCertificate(t *testing.T) {
	h := startAgentServer(t)
	now := time.Now()

	// Positive control on the same harness: a down server would make every rejection below
	// pass for the wrong reason.
	good := h.pki.Issue(t, agentID, now, time.Hour)
	if _, err := heartbeat(h.dial(t, &good.TLS), agentID.ServerID.String()); err != nil {
		t.Fatalf("positive control: %v", err)
	}

	t.Run("no client certificate", func(t *testing.T) {
		_, err := heartbeat(h.dial(t, nil), agentID.ServerID.String())
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %s (%v), want Unavailable (failed handshake)", status.Code(err), err)
		}
	})
	t.Run("plaintext client", func(t *testing.T) {
		conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, err = heartbeat(openvmsv1.NewNodeServiceClient(conn), agentID.ServerID.String())
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %s (%v), want Unavailable (failed handshake)", status.Code(err), err)
		}
	})
	t.Run("certificate from another CA", func(t *testing.T) {
		other := agentauthtest.NewPKI(t)
		c := other.Issue(t, agentID, now, time.Hour)
		h.pki.Store.Put(c.Record) // even a recorded serial must not help
		_, err := heartbeat(h.dial(t, &c.TLS), agentID.ServerID.String())
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %s (%v), want Unavailable (failed handshake)", status.Code(err), err)
		}
	})
	t.Run("expired certificate", func(t *testing.T) {
		c := h.pki.Issue(t, agentID, now.Add(-48*time.Hour), time.Hour)
		_, err := heartbeat(h.dial(t, &c.TLS), agentID.ServerID.String())
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %s (%v), want Unavailable (failed handshake)", status.Code(err), err)
		}
	})
	t.Run("record expired, certificate still valid for TLS", func(t *testing.T) {
		c := h.pki.Issue(t, agentID, now, time.Hour)
		h.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.NotAfter = now.Add(-time.Second) })
		_, err := heartbeat(h.dial(t, &c.TLS), agentID.ServerID.String())
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("code = %s (%v), want Unauthenticated", status.Code(err), err)
		}
	})
	t.Run("serial not issued by this installation", func(t *testing.T) {
		c := h.pki.IssueUnrecorded(t, h.pki.CA, agentID, now, time.Hour)
		_, err := heartbeat(h.dial(t, &c.TLS), agentID.ServerID.String())
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("code = %s (%v), want Unauthenticated", status.Code(err), err)
		}
	})
}

func TestAgentListener_RevocationAppliesToTheNextCallOnAnOpenConnection(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	client := h.dial(t, &c.TLS)
	if _, err := heartbeat(client, agentID.ServerID.String()); err != nil {
		t.Fatalf("before revocation: %v", err)
	}
	h.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
	_, err := heartbeat(client, agentID.ServerID.String())
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("after revocation: code = %s (%v), want Unauthenticated", status.Code(err), err)
	}
}

func TestAgentListener_HeartbeatIsBoundToTheCertificateIdentity(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	client := h.dial(t, &c.TLS)

	_, err := heartbeat(client, uuid.NewString())
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another server's node_id: code = %s (%v), want PermissionDenied", status.Code(err), err)
	}
	if _, err := heartbeat(client, ""); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty node_id: code = %s (%v), want InvalidArgument", status.Code(err), err)
	}
	if _, err := heartbeat(client, "not-a-uuid"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("malformed node_id: code = %s (%v), want InvalidArgument", status.Code(err), err)
	}
}

func TestAgentListener_ServesOnlyWhatAgentsNeed(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	client := h.dial(t, &c.TLS)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.ListNodes(ctx, &openvmsv1.ListNodesRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("ListNodes on the agent listener: code = %s (%v), want Unimplemented", status.Code(err), err)
	}
}

func TestNewAgentServerRefusesToRunWithoutMutualTLSOrVerifier(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pki.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentServer(AgentConfig{Verifier: &agentauth.Verifier{Certs: pki.Store}}); err == nil {
		t.Fatal("started without TLS credentials")
	}
	if _, err := NewAgentServer(AgentConfig{Credentials: creds}); err == nil {
		t.Fatal("started without a verifier")
	}
}

func TestAgentServerBindFailsWhenTheAddressIsTaken(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pki.Pool)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	srv, err := NewAgentServer(AgentConfig{Credentials: creds, Verifier: &agentauth.Verifier{Certs: pki.Store}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Bind(busy.Addr().String()); err == nil {
		t.Fatal("Bind succeeded on an address already in use")
	}
	if err := srv.Bind("not an address"); err == nil {
		t.Fatal("Bind succeeded on a malformed address")
	}
}
