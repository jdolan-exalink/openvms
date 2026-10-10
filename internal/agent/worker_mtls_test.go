package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/control"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

// recordingStore remembers which certificate serials the listener looked up, so a test can see
// which certificate each heartbeat was made with.
type recordingStore struct {
	inner  *agentauthtest.Store
	mu     sync.Mutex
	serial map[string]int
}

func (r *recordingStore) GetCertificate(ctx context.Context, serial string) (agentca.Certificate, error) {
	r.mu.Lock()
	if r.serial == nil {
		r.serial = map[string]int{}
	}
	r.serial[serial]++
	r.mu.Unlock()
	return r.inner.GetCertificate(ctx, serial)
}

func (r *recordingStore) ActivateCertificate(ctx context.Context, rec agentca.Certificate) error {
	return r.inner.ActivateCertificate(ctx, rec)
}

func (r *recordingStore) seen(serial string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.serial[serial]
}

// renewer signs with the PKI CA and records the certificate with its parent, like
// agentenroll.Service.Renew: it does not revoke the old certificate (the listener does that when
// the new one is first used). When dropResponse is set, the first renewal is recorded and then
// reported as failed, as if its response had been lost on the way to the agent.
type renewer struct {
	pki          *agentauthtest.PKI
	validity     time.Duration
	dropResponse *atomic.Bool
}

func (r renewer) Renew(_ context.Context, id agentca.Identity, previousSerial string, csrPEM []byte) (*agentca.Issuance, error) {
	res, err := (&agentca.Service{Validity: r.validity}).Sign(r.pki.CA, csrPEM, id)
	if err != nil {
		return nil, err
	}
	res.Record.ParentSerial = previousSerial
	r.pki.Store.Put(res.Record)
	if r.dropResponse != nil && r.dropResponse.CompareAndSwap(true, false) {
		return nil, errors.New("response lost")
	}
	return res, nil
}

type mtlsEnv struct {
	addr     string
	serverCA string
	pki      *agentauthtest.PKI
	store    *recordingStore
	id       agentca.Identity
}

// startAgentListener runs the real agent listener (verifier, revocation, renewal) on loopback.
func startAgentListener(t *testing.T) *mtlsEnv {
	t.Helper()
	return startAgentListenerDropping(t, nil)
}

// startAgentListenerDropping is startAgentListener whose renewer loses its first response when drop is set.
func startAgentListenerDropping(t *testing.T, drop *atomic.Bool) *mtlsEnv {
	t.Helper()
	pki := agentauthtest.NewPKI(t)
	rec := &recordingStore{inner: pki.Store}
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pki.Pool)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := control.NewAgentServer(control.AgentConfig{
		Credentials: creds,
		Verifier:    &agentauth.Verifier{Certs: rec},
		Renewer:     renewer{pki: pki, validity: time.Hour, dropResponse: drop},
	})
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.GRPCServer().Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &mtlsEnv{addr: lis.Addr().String(), serverCA: certFile, pki: pki, store: rec,
		id: agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}}
}

// manager turns an issued test certificate into the agent's credentials, persisted in a temp dir.
func (e *mtlsEnv) manager(t *testing.T, validity time.Duration) (*mtls.Manager, agentauthtest.Client) {
	t.Helper()
	c := e.pki.Issue(t, e.id, time.Now(), validity)
	key, err := x509.MarshalPKCS8PrivateKey(c.TLS.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	creds, err := mtls.NewCredentials(
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.TLS.Certificate[0]}),
		e.pki.CA.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	store := mtls.Store{Dir: t.TempDir()}
	if err := store.Save(creds); err != nil {
		t.Fatal(err)
	}
	return mtls.NewManager(store, creds), c
}

func (e *mtlsEnv) run(t *testing.T, cfg WorkerConfig) (*Worker, context.CancelFunc) {
	t.Helper()
	cfg.ControlGRPCAddr = e.addr
	cfg.TLSCAFile = e.serverCA
	cfg.HeartbeatInterval = 20 * time.Millisecond
	w := NewWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return w, cancel
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWorker_MTLSHeartbeatsWithTheCertificateIdentity(t *testing.T) {
	e := startAgentListener(t)
	mgr, _ := e.manager(t, time.Hour)
	// The configured node id is ignored: the identity is the one in the certificate, the only
	// server id the listener accepts.
	w, _ := e.run(t, WorkerConfig{NodeID: "node-auto", MTLS: mgr})
	if w.cfg.NodeID != e.id.ServerID.String() {
		t.Fatalf("node id = %q, want the certificate's server id %s", w.cfg.NodeID, e.id.ServerID)
	}
	eventually(t, "an acknowledged heartbeat", func() bool { return w.HeartbeatsAcknowledged() > 0 })
}

func TestWorker_MTLSRevokedCertificateIsNotAcknowledged(t *testing.T) {
	e := startAgentListener(t)
	mgr, c := e.manager(t, time.Hour)
	e.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
	w, _ := e.run(t, WorkerConfig{MTLS: mgr})
	eventually(t, "the listener to see the certificate", func() bool { return e.store.seen(c.Serial) > 0 })
	time.Sleep(100 * time.Millisecond)
	if n := w.HeartbeatsAcknowledged(); n != 0 {
		t.Fatalf("%d heartbeats acknowledged for a revoked certificate", n)
	}
}

func TestWorker_MTLSRenewsAndReconnectsWithTheNewCertificate(t *testing.T) {
	e := startAgentListener(t)
	// 1 minute left of a 6 minute lifetime: already past two thirds, so renewal is due at once.
	mgr, old := e.manager(t, time.Minute)
	w, _ := e.run(t, WorkerConfig{MTLS: mgr})

	eventually(t, "a renewed certificate", func() bool {
		return mgr.Current().Leaf.SerialNumber.Text(16) != old.Serial
	})
	renewed := mgr.Current().Leaf.SerialNumber.Text(16)
	if got := mgr.Current().Identity; got != e.id {
		t.Fatalf("renewed identity = %+v, want %+v", got, e.id)
	}
	// New connections present the renewed certificate: heartbeats are made with it.
	eventually(t, "a heartbeat with the renewed certificate", func() bool { return e.store.seen(renewed) > 1 })
	if w.HeartbeatsAcknowledged() == 0 {
		t.Fatal("no heartbeat was acknowledged")
	}
	// The renewed credentials are on disk for the next start.
	loaded, err := mgr.Store.Load()
	if err != nil || loaded.Leaf.SerialNumber.Text(16) != renewed {
		t.Fatalf("stored credentials = %v, %v; want the renewed certificate", loaded, err)
	}
}

// The renewed certificate supersedes the old one when it is first used, not before: once the
// worker has reconnected with it, the old certificate is revoked and heartbeats keep being
// acknowledged, which they could not be on a connection still using the old one.
func TestWorker_MTLSRevokesTheOldCertificateOnceTheRenewedOneIsUsed(t *testing.T) {
	e := startAgentListener(t)
	mgr, old := e.manager(t, time.Minute)
	w, _ := e.run(t, WorkerConfig{MTLS: mgr})

	eventually(t, "a renewed certificate", func() bool { return mgr.Current().Leaf.SerialNumber.Text(16) != old.Serial })
	renewed := mgr.Current().Leaf.SerialNumber.Text(16)
	eventually(t, "the old certificate to be revoked by the renewed one's first use", func() bool {
		rec, err := e.pki.Store.GetCertificate(context.Background(), old.Serial)
		return err == nil && rec.RevokedAt != nil
	})
	if e.store.seen(renewed) == 0 {
		t.Fatal("the old certificate was revoked but the renewed one never authenticated")
	}
	before := w.HeartbeatsAcknowledged()
	eventually(t, "heartbeats on the renewed certificate", func() bool { return w.HeartbeatsAcknowledged()-before >= 3 })
}

// The renewal response is lost: the server issued a certificate the agent never saw. The agent
// still holds a working old certificate, retries from it, and ends up on a certificate that works.
func TestWorker_MTLSRecoversFromALostRenewalResponse(t *testing.T) {
	drop := &atomic.Bool{}
	drop.Store(true)
	e := startAgentListenerDropping(t, drop)
	mgr, old := e.manager(t, time.Minute)
	mgr.MinBackoff, mgr.MaxBackoff = 50*time.Millisecond, 50*time.Millisecond
	w, _ := e.run(t, WorkerConfig{MTLS: mgr})

	eventually(t, "a renewed certificate after the lost response", func() bool { return mgr.Current().Leaf.SerialNumber.Text(16) != old.Serial })
	if drop.Load() {
		t.Fatal("the first renewal response was never dropped")
	}
	renewed := mgr.Current().Leaf.SerialNumber.Text(16)
	eventually(t, "the old certificate to be revoked", func() bool {
		rec, err := e.pki.Store.GetCertificate(context.Background(), old.Serial)
		return err == nil && rec.RevokedAt != nil
	})
	before := w.HeartbeatsAcknowledged()
	eventually(t, "heartbeats on the renewed certificate", func() bool {
		return w.HeartbeatsAcknowledged()-before >= 3 && e.store.seen(renewed) > 1
	})
	// The agent persisted what it uses.
	loaded, err := mgr.Store.Load()
	if err != nil || loaded.Leaf.SerialNumber.Text(16) != renewed {
		t.Fatalf("stored credentials = %v, %v; want the certificate in use", loaded, err)
	}
}

// A call that started on the replaced connection must be able to finish: the grace outlives both
// the renewal RPC and a heartbeat.
func TestOldConnectionGraceOutlivesTheLongestCall(t *testing.T) {
	if oldConnGrace <= mtls.RenewTimeout || oldConnGrace <= heartbeatTimeout {
		t.Fatalf("oldConnGrace = %v must exceed the renewal timeout %v and the heartbeat timeout %v", oldConnGrace, mtls.RenewTimeout, heartbeatTimeout)
	}
}
