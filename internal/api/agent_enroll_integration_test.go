//go:build integration

package api_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func enrollCSR(t *testing.T) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "x"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// TestAgentEnrollmentAPI covers the operator token endpoint and the unauthenticated enroll
// endpoint end to end: permissions, tenant isolation, one-time use and uniform failures.
func TestAgentEnrollmentAPI(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	h := &api.Handlers{
		Inv: env.Svc, Log: pgtest.Discard(),
		AgentEnroll: &agentenroll.Service{
			Store: env.Store, Authz: env.Svc,
			CA: &agentca.Service{Repo: &agentca.PgRepo{Store: env.Store}, Sealer: env.Sealer},
		},
	}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	do := func(method, path, bearer, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, strings.NewReader(body))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	server := env.Cameras["frigate-h01/acceso_norte"]
	tokenPath := "/api/v1/servers/" + server.ServerID.String() + "/agent/enroll-token"
	create := func() string {
		t.Helper()
		code, body := do("POST", tokenPath, env.AdminToken, "")
		if code != http.StatusCreated {
			t.Fatalf("create token: %d %s", code, body)
		}
		var out struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expires_at"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil || out.Token == "" || out.ExpiresAt == "" {
			t.Fatalf("create token body %s: %v", body, err)
		}
		return out.Token
	}
	enroll := func(token, csr string) (int, string) {
		b, _ := json.Marshal(map[string]string{"token": token, "csr_pem": csr})
		return do("POST", "/api/v1/agent/enroll", "", string(b)) // no session, no bearer
	}

	t.Run("create needs authentication", func(t *testing.T) {
		if code, _ := do("POST", tokenPath, "", ""); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated create: %d", code)
		}
	})

	t.Run("create needs the server permissions", func(t *testing.T) {
		code, _ := do("POST", tokenPath, env.Demo.Tokens["operador"], "")
		if code != http.StatusForbidden {
			t.Errorf("operator create: %d, want 403", code)
		}
	})

	t.Run("another tenant gets 404", func(t *testing.T) {
		other, err := env.Svc.CreateTenant(ctx, env.Admin, "otra-muni", "Otra Municipalidad")
		if err != nil {
			t.Fatal(err)
		}
		_, tokenB, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "owner-b")
		if err != nil {
			t.Fatal(err)
		}
		if code, body := do("POST", tokenPath, tokenB, ""); code != http.StatusNotFound {
			t.Errorf("cross-tenant create: %d %s, want 404", code, body)
		}
	})

	t.Run("enroll works without a session and the token is single use", func(t *testing.T) {
		token := create()
		code, body := enroll(token, enrollCSR(t))
		if code != http.StatusOK {
			t.Fatalf("enroll: %d %s", code, body)
		}
		var out struct {
			CertificatePEM string `json:"certificate_pem"`
			CAPEM          string `json:"ca_pem"`
			NotAfter       string `json:"not_after"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil || out.NotAfter == "" || !strings.Contains(out.CAPEM, "BEGIN CERTIFICATE") {
			t.Fatalf("enroll body %s: %v", body, err)
		}
		b, _ := pem.Decode([]byte(out.CertificatePEM))
		leaf, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		id, err := agentca.IdentityFromCert(leaf)
		if err != nil || id.ServerID != server.ServerID || id.TenantID != env.Demo.TenantID {
			t.Fatalf("identity = %+v, %v; want tenant %s server %s", id, err, env.Demo.TenantID, server.ServerID)
		}

		usedCode, usedBody := enroll(token, enrollCSR(t))
		unknownCode, unknownBody := enroll("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", enrollCSR(t))
		if usedCode != http.StatusUnauthorized || unknownCode != http.StatusUnauthorized {
			t.Fatalf("used/unknown: %d / %d, want 401", usedCode, unknownCode)
		}
		strip := func(s string) string { // request_id differs per request
			var m map[string]any
			_ = json.Unmarshal([]byte(s), &m)
			delete(m, "request_id")
			out, _ := json.Marshal(m)
			return string(out)
		}
		if strip(usedBody) != strip(unknownBody) {
			t.Errorf("used and unknown tokens answer differently: %s vs %s", usedBody, unknownBody)
		}
	})

	t.Run("a malformed CSR is 400 and keeps the token", func(t *testing.T) {
		token := create()
		if code, body := enroll(token, "junk"); code != http.StatusBadRequest {
			t.Fatalf("bad CSR: %d %s, want 400", code, body)
		}
		if code, body := enroll(token, enrollCSR(t)); code != http.StatusOK {
			t.Fatalf("retry after bad CSR: %d %s", code, body)
		}
	})

	t.Run("an oversized body is 400", func(t *testing.T) {
		code, _ := enroll("x", strings.Repeat("A", 20<<10))
		if code != http.StatusBadRequest {
			t.Errorf("oversized enroll body: %d, want 400", code)
		}
	})
}
