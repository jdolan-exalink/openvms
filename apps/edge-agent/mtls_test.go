package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

var mtlsTestID = agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}

// enrollAPI is an HTTPS stand-in for POST /api/v1/agent/enroll with one valid token.
type enrollAPI struct {
	srv     *httptest.Server
	caFile  string // PEM of the API's own TLS certificate, for OPENVMS_API_CA_FILE
	enrolls atomic.Int32
}

func newEnrollAPI(t *testing.T) *enrollAPI {
	t.Helper()
	ca, err := agentca.GenerateCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a := &enrollAPI{}
	a.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/v1/agent/enroll" || req["token"] != "one-time" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		issued, err := ca.SignCSR([]byte(req["csr_pem"]), mtlsTestID, time.Now(), 30*24*time.Hour)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.enrolls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"certificate_pem": string(issued.CertPEM), "ca_pem": string(ca.CertPEM)})
	}))
	t.Cleanup(a.srv.Close)
	a.caFile = filepath.Join(t.TempDir(), "api-ca.pem")
	if err := os.WriteFile(a.caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return a
}

func getenvFrom(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func (a *enrollAPI) env(t *testing.T, extra map[string]string) map[string]string {
	serverCA, _ := grpctlstest.WriteSelfSigned(t)
	env := map[string]string{
		"OPENVMS_AGENT_GRPC_ADDR":         "api.example:9443",
		"OPENVMS_API_URL":                 a.srv.URL,
		"OPENVMS_API_CA_FILE":             a.caFile,
		"OPENVMS_AGENT_STATE_DIR":         t.TempDir(),
		"OPENVMS_CONTROL_TLS_CA_FILE":     serverCA,
		"OPENVMS_CONTROL_TLS_SERVER_NAME": "api.example",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

var quiet = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func TestLoadMTLSIsOffWithoutTheAgentAddress(t *testing.T) {
	got, err := loadMTLS(context.Background(), getenvFrom(nil), quiet, time.Now)
	if err != nil || got != nil {
		t.Fatalf("loadMTLS = %v, %v; want nil, nil", got, err)
	}
}

func TestLoadMTLSRejectsSettingsThatWouldBeSilentlyIgnored(t *testing.T) {
	for _, env := range []map[string]string{
		{"OPENVMS_ENROLL_TOKEN": "x"},
		{"OPENVMS_ENROLL_TOKEN_FILE": "/x"},
		{"OPENVMS_API_URL": "https://api.example"},
		{"OPENVMS_API_CA_FILE": "/ca.pem"},
		{"OPENVMS_AGENT_STATE_DIR": "/var/lib/x"},
	} {
		if _, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now); err == nil {
			t.Errorf("%v accepted without OPENVMS_AGENT_GRPC_ADDR", env)
		}
	}
}

func TestLoadMTLSEnrollsOnceAndReusesTheStoredCertificate(t *testing.T) {
	api := newEnrollAPI(t)
	env := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN": "one-time"})

	got, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != "api.example:9443" || got.CAFile != env["OPENVMS_CONTROL_TLS_CA_FILE"] || got.ServerName != "api.example" {
		t.Fatalf("settings = %+v", got)
	}
	if got.Manager.Current().Identity != mtlsTestID {
		t.Fatalf("identity = %+v, want %+v", got.Manager.Current().Identity, mtlsTestID)
	}
	dir := filepath.Join(env["OPENVMS_AGENT_STATE_DIR"], "agent-mtls")
	if info, err := os.Stat(filepath.Join(dir, "key.pem")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key.pem = %v, %v; want a 0600 file in %s", info, err, dir)
	}

	// The next start has the same (spent) token configured and enrolls nothing.
	again, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if api.enrolls.Load() != 1 || again.Manager.Current().Leaf.SerialNumber.Cmp(got.Manager.Current().Leaf.SerialNumber) != 0 {
		t.Fatalf("enrolled %d times, or the stored certificate was replaced", api.enrolls.Load())
	}
	// And so does a start with no token at all.
	delete(env, "OPENVMS_ENROLL_TOKEN")
	delete(env, "OPENVMS_API_URL")
	if _, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now); err != nil {
		t.Fatalf("restart without a token: %v", err)
	}
}

func TestLoadMTLSReadsTheTokenFromAFile(t *testing.T) {
	api := newEnrollAPI(t)
	tokenFile := filepath.Join(t.TempDir(), "enroll.token")
	if err := os.WriteFile(tokenFile, []byte("one-time\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN_FILE": tokenFile})
	if _, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now); err != nil {
		t.Fatal(err)
	}
	both := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN_FILE": tokenFile, "OPENVMS_ENROLL_TOKEN": "one-time"})
	if _, err := loadMTLS(context.Background(), getenvFrom(both), quiet, time.Now); err == nil {
		t.Fatal("token and token file together were accepted")
	}
}

func TestLoadMTLSGuidesTheOperatorOnABadToken(t *testing.T) {
	api := newEnrollAPI(t)
	env := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN": "expired-or-used"})
	_, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now)
	if !errors.Is(err, mtls.ErrTokenRejected) {
		t.Fatalf("err = %v, want ErrTokenRejected", err)
	}
	if !strings.Contains(err.Error(), "OPENVMS_ENROLL_TOKEN") {
		t.Fatalf("the agent's error gives no guidance: %v", err)
	}
	if strings.Contains(err.Error(), "expired-or-used") {
		t.Fatal("the error echoes the token")
	}
}

func TestLoadMTLSNeedsACredentialSource(t *testing.T) {
	api := newEnrollAPI(t)
	env := api.env(t, nil)
	if _, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now); err == nil || !strings.Contains(err.Error(), "OPENVMS_ENROLL_TOKEN") {
		t.Fatalf("err = %v, want guidance naming OPENVMS_ENROLL_TOKEN", err)
	}
}

func TestLoadMTLSConfigurationErrors(t *testing.T) {
	api := newEnrollAPI(t)
	cases := map[string]map[string]string{
		"plain http API URL":     {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_API_URL": "http://api.example"},
		"missing API URL":        {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_API_URL": ""},
		"TLS explicitly off":     {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_CONTROL_TLS": "false"},
		"bad control TLS bool":   {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_CONTROL_TLS": "maybe"},
		"missing server CA file": {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_CONTROL_TLS_CA_FILE": "/nonexistent.pem"},
		"missing API CA file":    {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_API_CA_FILE": "/nonexistent.pem"},
		"missing token file":     {"OPENVMS_ENROLL_TOKEN_FILE": "/nonexistent.token"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			env := api.env(t, extra)
			if _, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now); err == nil {
				t.Fatal("accepted")
			}
			if api.enrolls.Load() != 0 {
				t.Fatal("enrolled despite the configuration error")
			}
		})
	}
}

// An API that cannot be reached (an agent behind NAT) is not a configuration error: loadMTLS says so
// with an error the caller can tell apart, so the agent keeps serving instead of exiting.
func TestLoadMTLSReportsAnUnreachableAPIAsRetryable(t *testing.T) {
	api := newEnrollAPI(t)
	env := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN": "one-time"})
	api.srv.Close()
	_, err := loadMTLS(context.Background(), getenvFrom(env), quiet, time.Now)
	if !errors.Is(err, mtls.ErrEnrollUnavailable) {
		t.Fatalf("err = %v, want ErrEnrollUnavailable", err)
	}
}

// Whatever the operator has to fix stays a hard error: the previous tests already pin the token and
// configuration cases; this one pins that they are not mistaken for "try again later".
func TestLoadMTLSConfigurationAndTokenErrorsAreNotRetryable(t *testing.T) {
	api := newEnrollAPI(t)
	for name, extra := range map[string]map[string]string{
		"bad token":      {"OPENVMS_ENROLL_TOKEN": "expired-or-used"},
		"plain http URL": {"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_API_URL": "http://api.example"},
		"no credentials": nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadMTLS(context.Background(), getenvFrom(api.env(t, extra)), quiet, time.Now)
			if err == nil || errors.Is(err, mtls.ErrEnrollUnavailable) {
				t.Fatalf("err = %v, want a non-retryable error", err)
			}
		})
	}
}

// retryMTLS keeps asking until the API answers, backing off between tries, and gives up on anything
// the operator has to fix.
func TestRetryMTLSKeepsTryingWhileTheAPIIsUnreachableThenSucceeds(t *testing.T) {
	calls := 0
	want := &mtlsSettings{Addr: "api.example:9443"}
	load := func() (*mtlsSettings, error) {
		calls++
		if calls < 3 {
			return nil, fmt.Errorf("enroll: %w", mtls.ErrEnrollUnavailable)
		}
		return want, nil
	}
	var waits []time.Duration
	got, err := retryMTLS(context.Background(), load, quiet, func(_ context.Context, d time.Duration) bool { waits = append(waits, d); return true })
	if err != nil || got != want || calls != 3 {
		t.Fatalf("got %v, %v after %d calls", got, err, calls)
	}
	if len(waits) != 2 || waits[1] <= waits[0] {
		t.Fatalf("waits = %v, want two growing waits", waits)
	}
}

func TestRetryMTLSStopsOnAnErrorTheOperatorMustFix(t *testing.T) {
	calls := 0
	load := func() (*mtlsSettings, error) { calls++; return nil, mtls.ErrTokenRejected }
	_, err := retryMTLS(context.Background(), load, quiet, func(context.Context, time.Duration) bool { return true })
	if !errors.Is(err, mtls.ErrTokenRejected) || calls != 1 {
		t.Fatalf("err = %v after %d calls, want ErrTokenRejected after 1", err, calls)
	}
}

func TestRetryMTLSStopsWhenTheAgentIsStopped(t *testing.T) {
	load := func() (*mtlsSettings, error) { return nil, mtls.ErrEnrollUnavailable }
	_, err := retryMTLS(context.Background(), load, quiet, func(context.Context, time.Duration) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRetryMTLSBackoffIsBounded(t *testing.T) {
	var last time.Duration
	for i := 0; i < 50; i++ {
		last = mtlsRetryDelay(i)
	}
	if last != mtlsRetryMax || mtlsRetryDelay(0) <= 0 {
		t.Fatalf("delay(0)=%v delay(49)=%v, want >0 and capped at %v", mtlsRetryDelay(0), last, mtlsRetryMax)
	}
}

// An API that accepts the connection and never answers (traffic dropped by a NAT) is the same case:
// the caller bounds the attempt with its context and gets the retryable error back.
func TestLoadMTLSReportsAnAPIThatNeverAnswersAsRetryable(t *testing.T) {
	api := newEnrollAPI(t)
	release := make(chan struct{})
	hang := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); hang.Close() })
	env := api.env(t, map[string]string{"OPENVMS_ENROLL_TOKEN": "one-time", "OPENVMS_API_URL": hang.URL})
	env["OPENVMS_API_CA_FILE"] = ""
	caFile := filepath.Join(t.TempDir(), "hang-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: hang.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	env["OPENVMS_API_CA_FILE"] = caFile
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := loadMTLS(ctx, getenvFrom(env), quiet, time.Now)
	if !errors.Is(err, mtls.ErrEnrollUnavailable) {
		t.Fatalf("err = %v, want ErrEnrollUnavailable", err)
	}
}
