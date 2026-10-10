package mtls

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// clock is a fake time source: Wait advances it instead of sleeping.
type clock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
	// stopAfter cancels the context once this many waits happened.
	stopAfter int
	cancel    context.CancelFunc
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	stop := c.stopAfter > 0 && len(c.waits) >= c.stopAfter
	c.mu.Unlock()
	if stop {
		c.cancel()
	}
	return ctx.Err()
}

type managerEnv struct {
	api   *fakeAPI
	mgr   *Manager
	clock *clock
	log   *bytes.Buffer
	ctx   context.Context
	// calls counts RenewFunc invocations; fail, when it returns an error, fails the call.
	calls int
	fail  func(call int) error
	// issueFor overrides the identity of the renewed certificate.
	issueFor agentca.Identity
	validity time.Duration
}

func newManagerEnv(t *testing.T, stopAfter int) *managerEnv {
	t.Helper()
	api := newFakeAPI(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	creds, err := Ensure(context.Background(), store, "good-token", api.enroll, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := &managerEnv{api: api, ctx: ctx, log: &bytes.Buffer{}, issueFor: testID, validity: 30 * 24 * time.Hour}
	e.clock = &clock{now: creds.NotBefore().Add(6 * time.Minute), stopAfter: stopAfter, cancel: cancel}
	e.mgr = NewManager(store, creds)
	e.mgr.Now = e.clock.Now
	e.mgr.Wait = e.clock.Wait
	e.mgr.Log = slog.New(slog.NewTextHandler(e.log, nil))
	return e
}

// renewFunc signs the CSR with the fake API's CA at the fake clock's time.
func (e *managerEnv) renewFunc() RenewFunc {
	return func(_ context.Context, csrPEM []byte) ([]byte, []byte, error) {
		e.calls++
		if e.fail != nil {
			if err := e.fail(e.calls); err != nil {
				return nil, nil, err
			}
		}
		issued, err := e.api.ca.SignCSR(csrPEM, e.issueFor, e.clock.Now(), e.validity)
		if err != nil {
			return nil, nil, err
		}
		return issued.CertPEM, e.api.ca.CertPEM, nil
	}
}

func TestRunRenewsAtTwoThirdsOfTheLifetime(t *testing.T) {
	e := newManagerEnv(t, 0)
	old := e.mgr.Current()
	due := old.RenewalDue()
	renewedAt := time.Time{}
	fn := e.renewFunc()
	e.mgr.Run(e.ctx, func(ctx context.Context, csr []byte) ([]byte, []byte, error) {
		renewedAt = e.clock.Now()
		defer e.clock.cancel() // one renewal is enough
		return fn(ctx, csr)
	})
	if renewedAt.Before(due) {
		t.Fatalf("renewed at %v, before the two-thirds point %v", renewedAt, due)
	}
	if renewedAt.Sub(due) > maxIdleWait {
		t.Fatalf("renewed %v after the two-thirds point; the loop sleeps at most %v at a time", renewedAt.Sub(due), maxIdleWait)
	}
	for _, w := range e.clock.waits {
		if w > maxIdleWait {
			t.Fatalf("waited %v in one sleep, want at most %v", w, maxIdleWait)
		}
	}
	if e.calls != 1 {
		t.Fatalf("renewed %d times, want 1", e.calls)
	}
}

func TestRenewReplacesAndPersistsTheCredentialsWithAFreshKey(t *testing.T) {
	e := newManagerEnv(t, 0)
	old := e.mgr.Current()
	if err := e.mgr.Renew(e.ctx, e.renewFunc()); err != nil {
		t.Fatal(err)
	}
	cur := e.mgr.Current()
	if cur.Leaf.SerialNumber.Cmp(old.Leaf.SerialNumber) == 0 {
		t.Fatal("the certificate was not replaced")
	}
	if bytes.Equal(cur.KeyPEM, old.KeyPEM) {
		t.Fatal("renewal reused the private key")
	}
	if cur.Identity != old.Identity {
		t.Fatalf("identity changed: %+v -> %+v", old.Identity, cur.Identity)
	}
	// New connections get the renewed certificate...
	got, err := e.mgr.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil || got.Leaf.SerialNumber.Cmp(cur.Leaf.SerialNumber) != 0 {
		t.Fatalf("GetClientCertificate = %v, %v; want the renewed certificate", got, err)
	}
	// ...and a restart finds it on disk.
	loaded, err := e.mgr.Store.Load()
	if err != nil || loaded.Leaf.SerialNumber.Cmp(cur.Leaf.SerialNumber) != 0 {
		t.Fatalf("stored credentials = %v, %v; want the renewed certificate", loaded, err)
	}
}

func TestRenewRefusesAnUnexpectedCertificate(t *testing.T) {
	other := agentca.Identity{TenantID: testID.TenantID, ServerID: uuid.New()}
	cases := map[string]func(e *managerEnv) RenewFunc{
		"another identity": func(e *managerEnv) RenewFunc { e.issueFor = other; return e.renewFunc() },
		"certificate for another key": func(e *managerEnv) RenewFunc {
			return func(ctx context.Context, _ []byte) ([]byte, []byte, error) {
				_, csr, _ := newKeyAndCSR()
				return e.renewFunc()(ctx, csr)
			}
		},
		"garbage": func(*managerEnv) RenewFunc {
			return func(context.Context, []byte) ([]byte, []byte, error) { return []byte("x"), []byte("y"), nil }
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			e := newManagerEnv(t, 0)
			old := e.mgr.Current()
			if err := e.mgr.Renew(e.ctx, mk(e)); err == nil {
				t.Fatal("accepted the certificate")
			}
			if e.mgr.Current() != old {
				t.Fatal("the credentials changed despite the refusal")
			}
			loaded, err := e.mgr.Store.Load()
			if err != nil || loaded.Leaf.SerialNumber.Cmp(old.Leaf.SerialNumber) != 0 {
				t.Fatal("the stored credentials changed despite the refusal")
			}
		})
	}
}

func TestRunRetriesWithBackoffAndKeepsTheOldCertificateMeanwhile(t *testing.T) {
	e := newManagerEnv(t, 0)
	e.mgr.MinBackoff, e.mgr.MaxBackoff = time.Minute, 5*time.Minute
	e.fail = func(call int) error {
		if call <= 4 {
			return errors.New("server unreachable")
		}
		e.clock.cancel() // the fifth attempt succeeds and ends the test
		return nil
	}
	old := e.mgr.Current()
	e.mgr.Run(e.ctx, e.renewFunc())

	if e.calls != 5 {
		t.Fatalf("%d attempts, want 5", e.calls)
	}
	// The waits after the first due time: 1m, 2m, 4m, 5m (capped).
	var retries []time.Duration
	for _, w := range e.clock.waits {
		if w <= 5*time.Minute {
			retries = append(retries, w)
		}
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute}
	if len(retries) < len(want) {
		t.Fatalf("retry waits = %v, want %v", retries, want)
	}
	for i, w := range want {
		if retries[len(retries)-len(want)+i] != w {
			t.Fatalf("retry waits = %v, want ...%v", retries, want)
		}
	}
	if e.mgr.Current().Leaf.SerialNumber.Cmp(old.Leaf.SerialNumber) == 0 {
		t.Fatal("the certificate was never replaced after the retries succeeded")
	}
	if !strings.Contains(e.log.String(), "server unreachable") {
		t.Fatalf("failures are not logged: %s", e.log.String())
	}
}

func TestRunLogsLoudlyOnceTheCertificateHasExpired(t *testing.T) {
	e := newManagerEnv(t, 0)
	e.mgr.MinBackoff, e.mgr.MaxBackoff = time.Hour, time.Hour
	e.fail = func(int) error { return errors.New("handshake: certificate expired") }
	// Start after expiry.
	e.clock.now = e.mgr.Current().NotAfter().Add(time.Minute)
	e.clock.stopAfter = 1
	e.mgr.Run(e.ctx, e.renewFunc())

	logs := e.log.String()
	if !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, "expired") || !strings.Contains(logs, "OPENVMS_ENROLL_TOKEN") {
		t.Fatalf("expiry is not reported loudly with the remedy:\n%s", logs)
	}
}

func TestRunDoesNotSpinWhenTheNewCertificateIsAlreadyDue(t *testing.T) {
	e := newManagerEnv(t, 6)
	e.mgr.MinBackoff = time.Minute
	e.validity = time.Minute // the server issues certificates that are due immediately
	e.clock.now = e.mgr.Current().RenewalDue()
	e.mgr.Run(e.ctx, e.renewFunc())
	if e.calls < 2 {
		t.Fatalf("%d renewals, want the loop to keep renewing at a bounded rate", e.calls)
	}
	for _, w := range e.clock.waits {
		if w < time.Minute {
			t.Fatalf("waited only %v between renewals, want at least the minimum backoff; waits=%v", w, e.clock.waits)
		}
	}
}

// The server refuses renewals before half of the lifetime (FailedPrecondition). That is a
// "retry later" answer: no error log, no expiry alarm, and the retries are spaced by the backoff
// instead of looping.
func TestRunTreatsTooEarlyAsRetryLater(t *testing.T) {
	e := newManagerEnv(t, 0)
	e.mgr.MinBackoff, e.mgr.MaxBackoff = time.Minute, 4*time.Minute
	e.clock.now = e.mgr.Current().RenewalDue()
	e.fail = func(call int) error {
		if call <= 3 {
			return status.Error(codes.FailedPrecondition, "certificate renewal not allowed yet")
		}
		e.clock.cancel()
		return nil
	}
	e.mgr.Run(e.ctx, e.renewFunc())

	if e.calls != 4 {
		t.Fatalf("%d attempts, want 4 (three refusals, then success)", e.calls)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute}
	if len(e.clock.waits) < 3 {
		t.Fatalf("waits = %v", e.clock.waits)
	}
	for i, w := range want {
		if e.clock.waits[i] != w {
			t.Fatalf("waits = %v, want the backoff %v", e.clock.waits, want)
		}
	}
	logs := e.log.String()
	if strings.Contains(logs, "level=ERROR") || strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "not allowed yet") {
		t.Fatalf("a too-early refusal must log at info, not as a failure:\n%s", logs)
	}
}
