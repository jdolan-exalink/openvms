package mtls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
)

const (
	defaultMinBackoff = 30 * time.Second
	defaultMaxBackoff = 15 * time.Minute
	// maxIdleWait caps a single sleep so the loop re-reads the clock regularly: a laptop
	// that was suspended or a clock that was corrected does not delay renewal by days.
	maxIdleWait = time.Hour
	renewRPC    = 30 * time.Second
)

// RenewFunc exchanges a CSR for a new certificate over an authenticated connection and
// returns the certificate and CA bundle PEM.
type RenewFunc func(ctx context.Context, csrPEM []byte) (certPEM, caPEM []byte, err error)

// GRPCRenew renews through AgentService.RenewCertificate on conn, which must present the
// current client certificate (the API identifies the caller by it alone).
func GRPCRenew(conn grpc.ClientConnInterface) RenewFunc {
	client := openvmsv1.NewAgentServiceClient(conn)
	return func(ctx context.Context, csrPEM []byte) ([]byte, []byte, error) {
		ctx, cancel := context.WithTimeout(ctx, renewRPC)
		defer cancel()
		resp, err := client.RenewCertificate(ctx, &openvmsv1.RenewCertificateRequest{CsrPem: string(csrPEM)})
		if err != nil {
			return nil, nil, err
		}
		return []byte(resp.CertificatePem), []byte(resp.CaPem), nil
	}
}

// Manager owns the agent's current credentials: it hands the newest certificate to each new
// connection and renews it when two thirds of its lifetime have passed.
type Manager struct {
	Store Store
	Log   *slog.Logger
	// Now and Wait are injectable for tests; they default to the wall clock and a timer.
	Now  func() time.Time
	Wait func(ctx context.Context, d time.Duration) error
	// MinBackoff and MaxBackoff bound the delay between failed renewals (30s and 15m by default).
	MinBackoff, MaxBackoff time.Duration

	creds atomic.Pointer[Credentials]
	gen   atomic.Uint64
}

// NewManager starts managing creds, which must already be persisted in store.
func NewManager(store Store, creds *Credentials) *Manager {
	m := &Manager{Store: store}
	m.creds.Store(creds)
	return m
}

// Generation increases every time the credentials are replaced, so a long-lived connection
// can tell that it was established with a certificate that has since been renewed.
func (m *Manager) Generation() uint64 { return m.gen.Load() }

// Current returns the credentials new connections use.
func (m *Manager) Current() *Credentials { return m.creds.Load() }

// GetClientCertificate is a tls.Config.GetClientCertificate: every new handshake gets the
// newest certificate, so a renewal needs no reconnect logic. Open connections keep the
// certificate they started with, which the server accepts until that certificate expires.
func (m *Manager) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	c := m.Current()
	if c == nil {
		return nil, errors.New("no agent certificate loaded")
	}
	return &c.TLS, nil
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

func (m *Manager) wait(ctx context.Context, d time.Duration) error {
	if m.Wait != nil {
		return m.Wait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (m *Manager) backoff(failures int) time.Duration {
	lo, hi := m.MinBackoff, m.MaxBackoff
	if lo <= 0 {
		lo = defaultMinBackoff
	}
	if hi <= 0 {
		hi = defaultMaxBackoff
	}
	d := lo
	for i := 1; i < failures && d < hi; i++ {
		d *= 2
	}
	return min(d, hi)
}

// Renew makes one renewal: a fresh key and CSR go through fn, the answer must be a
// certificate for that key and the same identity, and only then is it persisted and swapped
// in. Any failure leaves the current credentials and the files untouched.
func (m *Manager) Renew(ctx context.Context, fn RenewFunc) error {
	cur := m.Current()
	keyPEM, csrPEM, err := newKeyAndCSR()
	if err != nil {
		return err
	}
	certPEM, caPEM, err := fn(ctx, csrPEM)
	if err != nil {
		return err
	}
	next, err := NewCredentials(keyPEM, certPEM, caPEM)
	if err != nil {
		return fmt.Errorf("the API returned an unusable certificate: %w", err)
	}
	if next.Identity != cur.Identity {
		return fmt.Errorf("the API returned a certificate for another identity (server %s, want %s)", next.Identity.ServerID, cur.Identity.ServerID)
	}
	if err := m.Store.Save(next); err != nil {
		return fmt.Errorf("persist the renewed credentials: %w", err)
	}
	m.creds.Store(next)
	m.gen.Add(1)
	m.log().Info("agent certificate renewed", "serial", next.Leaf.SerialNumber.Text(16), "not_after", next.NotAfter())
	return nil
}

// Run renews the certificate whenever two thirds of its lifetime have passed, retrying
// failures with exponential backoff, until ctx is done. A certificate that expires without a
// successful renewal cannot be replaced over mTLS any more; each failed attempt then logs an
// error naming the remedy (a new enrollment token).
func (m *Manager) Run(ctx context.Context, fn RenewFunc) {
	failures := 0
	for ctx.Err() == nil {
		cur := m.Current()
		now := m.now()
		var wait time.Duration
		if due := cur.RenewalDue(); now.Before(due) {
			wait = min(due.Sub(now), maxIdleWait)
		} else if err := m.Renew(ctx, fn); err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			wait = m.backoff(failures)
			if status.Code(err) == codes.FailedPrecondition {
				// The server's minimum age for renewals has not passed (for example the agent's
				// clock runs ahead). Not a fault: try again later.
				m.log().Info("agent certificate renewal not allowed yet; will retry", "error", err, "retry_in", wait)
			} else if cur.Expired(now) {
				m.log().Error("agent certificate has expired and could not be renewed; the agent cannot reach the API over mTLS until it is enrolled again: create a new enrollment token for this server and restart the agent with OPENVMS_ENROLL_TOKEN",
					"not_after", cur.NotAfter(), "error", err, "retry_in", wait)
			} else {
				m.log().Warn("agent certificate renewal failed", "error", err, "not_after", cur.NotAfter(), "retry_in", wait)
			}
		} else {
			failures = 0
			// A certificate that is due the moment it is issued must not turn into a busy loop.
			if m.Current().RenewalDue().Before(m.now()) {
				wait = m.backoff(1)
			}
		}
		if wait > 0 {
			if err := m.wait(ctx, wait); err != nil {
				return
			}
		}
	}
}
