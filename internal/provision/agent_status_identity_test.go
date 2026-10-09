package provision

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
)

func TestClassifyAgentBinaryStatusSeparatesAvailabilityFromKnownMismatch(t *testing.T) {
	digestA := sha256.Sum256([]byte("agent A"))
	digestB := sha256.Sum256([]byte("agent B"))
	available := agent.BinaryIdentity{SHA256: hex.EncodeToString(digestA[:]), Architecture: "amd64", Version: "", Commit: ""}
	observedSame := available
	observedMismatch := agent.BinaryIdentity{SHA256: hex.EncodeToString(digestB[:]), Architecture: "amd64"}
	observedUnsupportedArch := agent.BinaryIdentity{SHA256: available.SHA256, Architecture: "bogus"}
	observedWhitespaceArch := agent.BinaryIdentity{SHA256: available.SHA256, Architecture: " amd64 "}
	observedDifferentArch := agent.BinaryIdentity{SHA256: available.SHA256, Architecture: "arm64"}
	tests := []struct {
		name             string
		installed        bool
		available        *agent.BinaryIdentity
		observed         *agent.BinaryIdentity
		wantState        AgentBinaryStatus
		wantOutdated     *bool
		wantUpgradeOffer bool
	}{
		{name: "not installed", available: &available, wantState: AgentBinaryNotInstalled},
		{name: "exact digest is current even without release labels", installed: true, available: &available, observed: &observedSame, wantState: AgentBinaryCurrent, wantOutdated: boolPtrTest(false)},
		{name: "different digest is available update", installed: true, available: &available, observed: &observedMismatch, wantState: AgentBinaryUpdateAvailable, wantOutdated: boolPtrTest(true), wantUpgradeOffer: true},
		{name: "unsupported observed architecture remains unknown", installed: true, available: &available, observed: &observedUnsupportedArch, wantState: AgentBinaryUnknown, wantUpgradeOffer: true},
		{name: "whitespace architecture remains unknown", installed: true, available: &available, observed: &observedWhitespaceArch, wantState: AgentBinaryUnknown, wantUpgradeOffer: true},
		{name: "same digest on different architecture is not current", installed: true, available: &available, observed: &observedDifferentArch, wantState: AgentBinaryUnknown, wantUpgradeOffer: true},
		{name: "legacy health missing identity remains unknown with upgrade path", installed: true, available: &available, wantState: AgentBinaryUnknown, wantUpgradeOffer: true},
		{name: "desired artifact unavailable is unknown", installed: true, observed: &observedSame, wantState: AgentBinaryUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAgentBinaryStatus(tt.installed, tt.available, tt.observed, false)
			if got.Status != tt.wantState || !equalOptionalBoolTest(got.Outdated, tt.wantOutdated) || got.UpgradeAvailable != tt.wantUpgradeOffer {
				t.Fatalf("status = %#v, want state=%q outdated=%v upgrade=%v", got, tt.wantState, tt.wantOutdated, tt.wantUpgradeOffer)
			}
		})
	}
}

func boolPtrTest(value bool) *bool { return &value }

func equalOptionalBoolTest(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestClassifyAgentBinaryStatusReportsUnreachableWithoutFalseMismatch(t *testing.T) {
	digest := sha256.Sum256([]byte("agent"))
	available := agent.BinaryIdentity{SHA256: hex.EncodeToString(digest[:]), Architecture: "amd64"}
	got := classifyAgentBinaryStatus(true, &available, nil, true)
	if got.Status != AgentBinaryUnreachable || got.Outdated != nil || !got.UpgradeAvailable {
		t.Fatalf("unreachable status = %#v", got)
	}
}

func TestDesiredStatusAndUpdateUseOneCachedTrustedArtifactResolver(t *testing.T) {
	binary := []byte("same trusted artifact")
	identity := agent.BinaryIdentity{SHA256: strings.Repeat("c", 64), Architecture: "amd64", Version: "v1.0.0"}
	calls := 0
	svc := &Service{agentStatusArtifactLoader: func() (AgentArtifact, error) {
		calls++
		return AgentArtifact{Binary: binary, Identity: identity}, nil
	}}
	statusArtifact, err := svc.desiredAgentArtifact()
	if err != nil {
		t.Fatal(err)
	}
	updateBinary, err := svc.loadAgentInstallBinary()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || string(statusArtifact.Binary) != string(binary) || string(updateBinary) != string(binary) || statusArtifact.Identity != identity {
		t.Fatalf("artifact resolver calls=%d status=%#v update=%q", calls, statusArtifact, updateBinary)
	}
}

func TestFetchVerifiedAgentIdentityUsesOnlyRegisteredHTTPSAuthority(t *testing.T) {
	identity := agent.BinaryIdentity{SHA256: strings.Repeat("a", 64), Architecture: "amd64", Version: "v1.2.0", Commit: "deadbeef"}
	body := fmt.Sprintf(`{"status":"ok","version":"0.1.0","binary_identity":{"sha256":%q,"architecture":%q,"version":%q,"commit":%q}}`, identity.SHA256, identity.Architecture, identity.Version, identity.Commit)
	var calls int
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Scheme != "https" || request.URL.Host != "10.30.0.7:9443" || request.URL.Path != "/v1/health" || request.URL.RawQuery != "" {
			t.Fatalf("request escaped registered HTTPS authority: %s", request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer token-secret" {
			t.Fatalf("bearer header = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cfg := AgentTLSConfig{SecurePort: 9443, TrustMode: AgentTLSTrustCustom, CAPEM: testCAPEMIdentity(root)}
	got, err := fetchVerifiedAgentIdentity(context.Background(), "10.30.0.7", "token-secret", cfg, transport)
	if err != nil {
		t.Fatal(err)
	}
	if got != identity || calls != 1 {
		t.Fatalf("identity/calls = %#v/%d, want %#v/1", got, calls, identity)
	}
}

func TestFetchVerifiedAgentIdentityRejectsUntrustedOrMalformedHealth(t *testing.T) {
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized health", status: http.StatusUnauthorized, body: `{"status":"ok"}`},
		{name: "missing digest", status: http.StatusOK, body: `{"status":"ok","binary_identity":{"architecture":"amd64"}}`},
		{name: "invalid digest", status: http.StatusOK, body: `{"status":"ok","binary_identity":{"sha256":"short","architecture":"amd64"}}`},
		{name: "unsupported architecture", status: http.StatusOK, body: `{"status":"ok","binary_identity":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","architecture":"bogus"}}`},
		{name: "whitespace architecture", status: http.StatusOK, body: `{"status":"ok","binary_identity":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","architecture":" amd64 "}}`},
		{name: "malformed JSON", status: http.StatusOK, body: `not-json`},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", agent.MaxHealthResponseBytes+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			cfg := AgentTLSConfig{SecurePort: 9443, TrustMode: AgentTLSTrustCustom, CAPEM: testCAPEMIdentity(root)}
			if _, err := fetchVerifiedAgentIdentity(context.Background(), "10.30.0.7", "token", cfg, transport); err == nil {
				t.Fatal("unverified health response accepted")
			}
		})
	}
}

func TestFetchVerifiedAgentIdentityDoesNotFallbackWhenTrustIsMissing(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	})
	cfg := AgentTLSConfig{SecurePort: 9443, TrustMode: AgentTLSTrustCustom}
	if _, err := fetchVerifiedAgentIdentity(context.Background(), "10.30.0.7", "token", cfg, transport); err == nil {
		t.Fatal("health request succeeded without configured trust")
	}
	if calls != 0 {
		t.Fatalf("transport calls = %d, want zero when trust is missing", calls)
	}
}

func testCAPEMIdentity(root *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
}

func TestLegacyUpdatePayloadUsesTrustedArtifactAndNeverFallsBack(t *testing.T) {
	want := []byte("trusted packaged agent")
	fallbackCalled := false
	svc := &Service{
		Binary: func() ([]byte, error) { fallbackCalled = true; return []byte("different source-build fallback"), nil },
		agentStatusArtifactLoader: func() (AgentArtifact, error) {
			return AgentArtifact{Binary: want, Identity: agent.BinaryIdentity{SHA256: strings.Repeat("d", 64), Architecture: "amd64"}}, nil
		},
	}
	got, err := svc.loadLegacyAgentUpdateBinary()
	if err != nil {
		t.Fatal(err)
	}
	if fallbackCalled || string(got) != string(want) {
		t.Fatalf("update payload=%q fallbackCalled=%v", got, fallbackCalled)
	}
}

func TestLegacyUpdatePayloadFailsClosedWhenTrustedArtifactUnavailable(t *testing.T) {
	fallbackCalled := false
	svc := &Service{
		Binary:                    func() ([]byte, error) { fallbackCalled = true; return []byte("untrusted fallback"), nil },
		agentStatusArtifactLoader: func() (AgentArtifact, error) { return AgentArtifact{}, errors.New("manifest unavailable") },
	}
	if _, err := svc.loadLegacyAgentUpdateBinary(); err == nil {
		t.Fatal("legacy update accepted unverified fallback artifact")
	}
	if fallbackCalled {
		t.Fatal("legacy update invoked untrusted fallback")
	}
}
