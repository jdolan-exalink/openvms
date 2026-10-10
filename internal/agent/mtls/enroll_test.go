package mtls

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agentca"
)

var testID = agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}

// fakeAPI answers /api/v1/agent/enroll like the real handler: one valid token, a CA that signs
// the CSR's public key for testID.
type fakeAPI struct {
	srv      *httptest.Server
	ca       *agentca.CA
	enrolls  atomic.Int32
	validity time.Duration
	// mutate, when set, edits the reply before it is sent.
	mutate func(map[string]string)
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	ca, err := agentca.GenerateCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{ca: ca, validity: 30 * 24 * time.Hour}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Token, CSRPEM string }
		var raw map[string]string
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Token, req.CSRPEM = raw["token"], raw["csr_pem"]
		if r.URL.Path != "/api/v1/agent/enroll" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if req.Token != "good-token" && req.Token != "second-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		issued, err := ca.SignCSR([]byte(req.CSRPEM), testID, time.Now(), f.validity)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.enrolls.Add(1)
		reply := map[string]string{"certificate_pem": string(issued.CertPEM), "ca_pem": string(ca.CertPEM)}
		if f.mutate != nil {
			f.mutate(reply)
		}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) enroll(ctx context.Context, token string) (*Credentials, error) {
	return Enroll(ctx, f.srv.Client(), f.srv.URL+"/api/v1/agent/enroll", token)
}

func TestEnrollReturnsVerifiedCredentials(t *testing.T) {
	api := newFakeAPI(t)
	creds, err := api.enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Identity != testID {
		t.Fatalf("identity = %+v, want %+v", creds.Identity, testID)
	}
	if !strings.Contains(string(creds.KeyPEM), "PRIVATE KEY") {
		t.Fatal("no private key generated locally")
	}
}

func TestEnrollTokenRejected(t *testing.T) {
	api := newFakeAPI(t)
	_, err := api.enroll(context.Background(), "wrong-token")
	if !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("err = %v, want ErrTokenRejected", err)
	}
	if strings.Contains(err.Error(), "wrong-token") {
		t.Fatal("the error echoes the token")
	}
}

func TestEnrollRefusesAnUntrustworthyReply(t *testing.T) {
	cases := map[string]func(map[string]string){
		"certificate for another key": func(r map[string]string) {
			// Sign a different key with the same CA.
			other := newFakeAPI(t)
			c, err := other.enroll(context.Background(), "good-token")
			if err != nil {
				t.Fatal(err)
			}
			r["certificate_pem"] = string(c.CertPEM)
		},
		"certificate from a CA the reply does not carry": func(r map[string]string) {
			ca, _ := agentca.GenerateCA(time.Now())
			r["ca_pem"] = string(ca.CertPEM)
		},
		"empty CA": func(r map[string]string) { r["ca_pem"] = "" },
		"garbage":  func(r map[string]string) { r["certificate_pem"] = "nope" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.mutate = mutate
			if _, err := api.enroll(context.Background(), "good-token"); err == nil {
				t.Fatal("accepted the reply")
			}
		})
	}
}

func TestEnrollServerError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := Enroll(context.Background(), srv.Client(), srv.URL, "tok-secret")
	if err == nil || errors.Is(err, ErrTokenRejected) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want a 500 error", err)
	}
	if strings.Contains(err.Error(), "tok-secret") {
		t.Fatal("the error echoes the token")
	}
}

func TestEnrollURLRequiresHTTPS(t *testing.T) {
	got, err := EnrollURL("https://api.example.com/base/")
	if err != nil || got != "https://api.example.com/base/api/v1/agent/enroll" {
		t.Fatalf("EnrollURL = %q, %v", got, err)
	}
	for _, bad := range []string{"http://api.example.com", "", "api.example.com", "ftp://x"} {
		if _, err := EnrollURL(bad); err == nil {
			t.Fatalf("EnrollURL(%q) accepted", bad)
		}
	}
}

func TestAPIClientVerifiesTheServerCertificate(t *testing.T) {
	api := newFakeAPI(t)
	url := api.srv.URL + "/api/v1/agent/enroll"

	// System roots do not know the test server's certificate: refused, never skipped.
	hc, err := NewAPIClient("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Enroll(context.Background(), hc, url, "good-token"); err == nil {
		t.Fatal("enrolled against a server with an untrusted certificate")
	}

	// An explicit CA file that trusts it works.
	caFile := filepath.Join(t.TempDir(), "api-ca.pem")
	leaf := api.srv.Certificate()
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	hc, err = NewAPIClient(caFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Enroll(context.Background(), hc, url, "good-token"); err != nil {
		t.Fatalf("with the API CA file: %v", err)
	}

	if _, err := NewAPIClient(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("missing CA file accepted")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	_ = os.WriteFile(empty, []byte("x"), 0o600)
	if _, err := NewAPIClient(empty); err == nil {
		t.Fatal("CA file without certificates accepted")
	}
}

func fixedNow(ts time.Time) func() time.Time { return func() time.Time { return ts } }

func TestEnsureEnrollsOnceAndPersists(t *testing.T) {
	api := newFakeAPI(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	ctx := context.Background()

	creds, err := Ensure(ctx, store, "good-token", api.enroll, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{"key.pem": 0o600, "cert.pem": 0o644, "ca.pem": 0o644, "enroll-token.sha256": 0o600} {
		info, err := os.Stat(filepath.Join(store.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), mode)
		}
	}
	if info, _ := os.Stat(store.Dir); info.Mode().Perm() != 0o700 {
		t.Errorf("state dir mode = %o, want 700", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(store.Dir); len(entries) != 4 {
		t.Errorf("state dir has %d entries, want exactly key, cert, ca and the token hash", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(store.Dir, "enroll-token.sha256"))
	if strings.Contains(string(raw), "good-token") {
		t.Error("the token is stored in the clear")
	}

	// A restart with the same (now spent) token reuses the stored certificate.
	again, err := Ensure(ctx, store, "good-token", api.enroll, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if again.Leaf.SerialNumber.Cmp(creds.Leaf.SerialNumber) != 0 {
		t.Fatal("reloaded a different certificate")
	}
	if n := api.enrolls.Load(); n != 1 {
		t.Fatalf("enrolled %d times, want 1", n)
	}
	// And without any token at all.
	if _, err := Ensure(ctx, store, "", api.enroll, time.Now); err != nil {
		t.Fatalf("restart without a token: %v", err)
	}
}

func TestEnsureWithoutCredentialsOrTokenExplains(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	_, err := Ensure(context.Background(), store, "", nil, time.Now)
	if err == nil || !strings.Contains(err.Error(), "OPENVMS_ENROLL_TOKEN") {
		t.Fatalf("err = %v, want guidance naming OPENVMS_ENROLL_TOKEN", err)
	}
}

func TestEnsureFailedEnrollmentStoresNothing(t *testing.T) {
	api := newFakeAPI(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	if _, err := Ensure(context.Background(), store, "wrong", api.enroll, time.Now); !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("err = %v, want ErrTokenRejected", err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("Load after a failed enrollment = %v, want ErrNoCredentials", err)
	}
}

func TestEnsureCorruptStateIsAnErrorNotAReEnrollment(t *testing.T) {
	api := newFakeAPI(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	if _, err := Ensure(context.Background(), store, "good-token", api.enroll, time.Now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "cert.pem"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(context.Background(), store, "good-token", api.enroll, time.Now); err == nil {
		t.Fatal("corrupt state was accepted")
	}
	if n := api.enrolls.Load(); n != 1 {
		t.Fatalf("enrolled %d times after corruption, want 1", n)
	}
}

func TestEnsureReEnrollsAnExpiredCertificateOnlyWithANewToken(t *testing.T) {
	api := newFakeAPI(t)
	api.validity = time.Hour
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	ctx := context.Background()
	if _, err := Ensure(ctx, store, "good-token", api.enroll, time.Now); err != nil {
		t.Fatal(err)
	}
	later := fixedNow(time.Now().Add(48 * time.Hour))

	// Expired, but the configured token is the one already spent: keep the stored identity.
	creds, err := Ensure(ctx, store, "good-token", api.enroll, later)
	if err != nil || !creds.Expired(later()) {
		t.Fatalf("Ensure = %v, %v; want the expired stored certificate", creds, err)
	}
	if api.enrolls.Load() != 1 {
		t.Fatal("re-enrolled with a spent token")
	}

	// A fresh token recovers the agent, and is itself remembered as spent.
	api.validity = 30 * 24 * time.Hour
	fresh, err := Ensure(ctx, store, "second-token", api.enroll, later)
	if err != nil || fresh.Expired(later()) {
		t.Fatalf("Ensure with a new token = %v, %v; want a fresh certificate", fresh, err)
	}
	if !store.TokenUsed("second-token") || api.enrolls.Load() != 2 {
		t.Fatal("the new token was not redeemed and recorded")
	}
	if _, err := Ensure(ctx, store, "second-token", api.enroll, later); err != nil || api.enrolls.Load() != 2 {
		t.Fatalf("restart re-enrolled: %v (%d enrolls)", err, api.enrolls.Load())
	}
}

func TestStoreLoadFinishesAnInterruptedSave(t *testing.T) {
	api := newFakeAPI(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	old, err := Ensure(context.Background(), store, "good-token", api.enroll, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	// A second save is interrupted after the key rename: new key and old certificate in place,
	// the new certificate staged.
	next, err := api.enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "key.pem"), next.KeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "cert.pem.new"), next.CertPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after an interrupted save: %v", err)
	}
	if got.Leaf.SerialNumber.Cmp(next.Leaf.SerialNumber) != 0 || got.Leaf.SerialNumber.Cmp(old.Leaf.SerialNumber) == 0 {
		t.Fatal("Load did not finish the interrupted save")
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "cert.pem.new")); !os.IsNotExist(err) {
		t.Fatal("the staged certificate was left behind")
	}
}

func TestRenewalDueIsTwoThirdsOfTheLifetime(t *testing.T) {
	api := newFakeAPI(t)
	creds, err := api.enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	life := creds.NotAfter().Sub(creds.NotBefore())
	want := creds.NotBefore().Add(life * 2 / 3)
	if !creds.RenewalDue().Equal(want) {
		t.Fatalf("RenewalDue = %v, want %v", creds.RenewalDue(), want)
	}
	if d := creds.RenewalDue().Sub(creds.NotBefore()); d < 19*24*time.Hour || d > 21*24*time.Hour {
		t.Fatalf("renewal due %v after NotBefore, want about 20 days of 30", d)
	}
}

// A redirect would re-send the token (in the request body, for 307/308) to wherever the
// redirect points, possibly over a weaker scheme. Enroll refuses to follow redirects.
func TestEnrollNeverFollowsARedirect(t *testing.T) {
	var gotToken atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken.Add(1)
		http.Error(w, "should never be reached", http.StatusTeapot)
	}))
	defer target.Close()
	for _, code := range []int{http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/api/v1/agent/enroll", code)
		}))
		_, err := Enroll(context.Background(), first.Client(), first.URL+"/api/v1/agent/enroll", "secret-token")
		first.Close()
		if err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Fatalf("HTTP %d: err = %v, want a redirect error", code, err)
		}
		if strings.Contains(err.Error(), "secret-token") {
			t.Fatal("the error echoes the token")
		}
	}
	if n := gotToken.Load(); n != 0 {
		t.Fatalf("the redirect target received %d requests carrying the token", n)
	}
}

// The state directory is checked before the one-time token is redeemed: a read-only or
// otherwise unusable directory must not burn the token.
func TestEnsureChecksTheStateDirectoryBeforeRedeemingTheToken(t *testing.T) {
	api := newFakeAPI(t)
	var requests atomic.Int32
	enroll := func(ctx context.Context, token string) (*Credentials, error) {
		requests.Add(1)
		return api.enroll(ctx, token)
	}
	// A regular file where the directory should be: MkdirAll fails whoever runs the test.
	blocker := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(blocker, "agent-mtls")}
	_, err := Ensure(context.Background(), store, "good-token", enroll, time.Now)
	if err == nil || !strings.Contains(err.Error(), "state directory") {
		t.Fatalf("err = %v, want a clear state directory error", err)
	}
	if requests.Load() != 0 || api.enrolls.Load() != 0 {
		t.Fatal("the enroll endpoint was called although the credentials could not be stored")
	}
	if store.TokenUsed("good-token") {
		t.Fatal("the token was recorded as used")
	}
}

func TestErrTokenRejectedIsLibraryNeutral(t *testing.T) {
	msg := ErrTokenRejected.Error()
	if strings.Contains(msg, "OPENVMS_") || strings.Contains(msg, "15 minutes") {
		t.Fatalf("the library error hardcodes deployment details: %q", msg)
	}
}
