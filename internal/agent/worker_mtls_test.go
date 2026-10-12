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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// run starts a Worker driven by hand (see beatDriver) and returns it with its driver. The
// renewal loop runs on the manager's own injected waits, never on real sleeps.
func (e *mtlsEnv) run(t *testing.T, cfg WorkerConfig) (*Worker, *beatDriver) {
	t.Helper()
	cfg.ControlGRPCAddr = e.addr
	cfg.TLSCAFile = e.serverCA
	d := newBeatDriver()
	d.wire(&cfg)
	w := NewWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return w, d
}

// instantWaits makes the manager's retry waits return at once, so a retry happens immediately
// and in order instead of after a real backoff.
func instantWaits(mgr *mtls.Manager) {
	mgr.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
}

// renewals returns a channel of the certificates the manager renews to.
func renewals(mgr *mtls.Manager) func() *mtls.Credentials {
	ch := make(chan *mtls.Credentials, 16)
	mgr.OnRenew = func(c *mtls.Credentials) { ch <- c }
	return func() *mtls.Credentials {
		select {
		case c := <-ch:
			return c
		case <-time.After(failsafe):
			panic("timed out waiting for a renewal")
		}
	}
}

func serial(c *mtls.Credentials) string { return c.Leaf.SerialNumber.Text(16) }

func TestWorker_MTLSHeartbeatsWithTheCertificateIdentity(t *testing.T) {
	e := startAgentListener(t)
	mgr, _ := e.manager(t, time.Hour)
	// The configured node id is ignored: the identity is the one in the certificate, the only
	// server id the listener accepts.
	w, d := e.run(t, WorkerConfig{NodeID: "node-auto", MTLS: mgr})
	if w.cfg.NodeID != e.id.ServerID.String() {
		t.Fatalf("node id = %q, want the certificate's server id %s", w.cfg.NodeID, e.id.ServerID)
	}
	if err := d.next(t); err != nil {
		t.Fatalf("initial heartbeat: %v", err)
	}
	if w.HeartbeatsAcknowledged() != 1 {
		t.Fatalf("%d acknowledged heartbeats, want 1", w.HeartbeatsAcknowledged())
	}
}

func TestWorker_MTLSRevokedCertificateIsNotAcknowledged(t *testing.T) {
	e := startAgentListener(t)
	mgr, c := e.manager(t, time.Hour)
	e.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
	w, d := e.run(t, WorkerConfig{MTLS: mgr})
	for i := range 3 { // the initial heartbeat and two ticks
		var err error
		if i == 0 {
			err = d.next(t)
		} else {
			err = d.tick(t)
		}
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("attempt %d: %v, want Unauthenticated", i+1, err)
		}
	}
	if n := w.HeartbeatsAcknowledged(); n != 0 {
		t.Fatalf("%d heartbeats acknowledged for a revoked certificate", n)
	}
	if e.store.seen(c.Serial) < 3 {
		t.Fatal("the listener did not see every attempt")
	}
}

// A renewal swaps the certificate; the next tick reconnects and the heartbeat after it is made
// with the new certificate, whose first use revokes the old one.
func TestWorker_MTLSRenewsReconnectsAndRevokesTheOldCertificate(t *testing.T) {
	e := startAgentListener(t)
	// 1 minute left of a 6 minute lifetime: already past two thirds, so renewal is due at once.
	mgr, old := e.manager(t, time.Minute)
	waitRenewal := renewals(mgr)
	w, d := e.run(t, WorkerConfig{MTLS: mgr})

	renewed := waitRenewal()
	if serial(renewed) == old.Serial {
		t.Fatal("renewal kept the old certificate")
	}
	if renewed.Identity != e.id {
		t.Fatalf("renewed identity = %+v, want %+v", renewed.Identity, e.id)
	}
	_ = d.next(t) // the initial heartbeat, with whichever certificate was current
	if err := d.tick(t); err != nil {
		t.Fatalf("heartbeat after the renewal: %v", err)
	}
	if e.store.seen(serial(renewed)) == 0 {
		t.Fatal("the heartbeat after the renewal did not use the renewed certificate")
	}
	rec, err := e.pki.Store.GetCertificate(context.Background(), old.Serial)
	if err != nil || rec.RevokedAt == nil {
		t.Fatalf("old certificate record = %+v, %v; want it revoked by the renewed one's first use", rec, err)
	}
	before := w.HeartbeatsAcknowledged()
	if err := d.tick(t); err != nil || w.HeartbeatsAcknowledged() != before+1 {
		t.Fatalf("next heartbeat: %v (acked %d -> %d)", err, before, w.HeartbeatsAcknowledged())
	}
	// The renewed credentials are on disk for the next start.
	loaded, err := mgr.Store.Load()
	if err != nil || serial(loaded) != serial(renewed) {
		t.Fatalf("stored credentials = %v, %v; want the renewed certificate", loaded, err)
	}
}

// The renewal response is lost: the server issued a certificate the agent never saw. The agent
// still holds a working old certificate, retries from it, and ends up on a certificate that works.
func TestWorker_MTLSRecoversFromALostRenewalResponse(t *testing.T) {
	drop := &atomic.Bool{}
	drop.Store(true)
	e := startAgentListenerDropping(t, drop)
	mgr, old := e.manager(t, time.Minute)
	instantWaits(mgr)
	waitRenewal := renewals(mgr)
	w, d := e.run(t, WorkerConfig{MTLS: mgr})

	renewed := waitRenewal() // only the retry succeeds
	if drop.Load() {
		t.Fatal("the first renewal response was never dropped")
	}
	if serial(renewed) == old.Serial {
		t.Fatal("renewal kept the old certificate")
	}
	_ = d.next(t)
	if err := d.tick(t); err != nil {
		t.Fatalf("heartbeat after the retried renewal: %v", err)
	}
	rec, err := e.pki.Store.GetCertificate(context.Background(), old.Serial)
	if err != nil || rec.RevokedAt == nil {
		t.Fatalf("old certificate record = %+v, %v; want it revoked by the renewed one's first use", rec, err)
	}
	if e.store.seen(serial(renewed)) == 0 || w.HeartbeatsAcknowledged() == 0 {
		t.Fatal("the agent never worked on the renewed certificate")
	}
	loaded, err := mgr.Store.Load()
	if err != nil || serial(loaded) != serial(renewed) {
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
