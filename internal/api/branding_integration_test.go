//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

var errNotFound = errors.New("object not found")

// memBlobs is an in-memory object store fake, used so the branding logo roundtrip
// (PUT -> GET .../logo) can be verified without a real S3-compatible backend.
type memBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
	ct   map[string]string
}

func (b *memBlobs) Put(_ context.Context, key string, body []byte, contentType string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data == nil {
		b.data, b.ct = map[string][]byte{}, map[string]string{}
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	b.data[key], b.ct[key] = cp, contentType
	return nil
}

func (b *memBlobs) Get(_ context.Context, key string) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[key]
	if !ok {
		return nil, "", errNotFound
	}
	return d, b.ct[key], nil
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTenantBrandingAPI covers PDW-1: reading branding needs no special permission within
// the tenant, writing it needs tenant.manage, another tenant's branding 404s, and every
// write is audited (BRANDING_UPDATED / BRANDING_REMOVED).
func TestTenantBrandingAPI(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()

	adapters := inventory.NewAdapters(env.Svc)
	mediaSvc := &media.Service{Store: env.Store, Adapters: adapters, Log: pgtest.Discard()}
	blobs := &memBlobs{}
	brandingSvc := &branding.Service{Store: env.Store, Blobs: blobs, Log: pgtest.Discard()}
	h := &api.Handlers{Inv: env.Svc, Media: mediaSvc, Branding: brandingSvc, Log: pgtest.Discard()}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	do := func(t *testing.T, token, method, path, body string) (int, map[string]any) {
		t.Helper()
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, r)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	operatorToken := env.Demo.Tokens["operador"]
	brandingPath := "/api/v1/tenants/" + env.Demo.TenantID.String() + "/branding"

	// A tenant with no branding yet answers 200 with empty fields, not 404 — reachable by an
	// ordinary tenant member with no special permission.
	code, body := do(t, operatorToken, "GET", brandingPath, "")
	if code != http.StatusOK {
		t.Fatalf("get empty branding: %d %v", code, body)
	}
	if body["owner_name"] != "" || body["has_logo"] != false {
		t.Errorf("empty branding = %v, want owner_name=\"\" has_logo=false", body)
	}

	// An actor without tenant.manage cannot write branding.
	code, body = do(t, operatorToken, "PUT", brandingPath, `{"owner_name":"Municipalidad"}`)
	if code != http.StatusForbidden {
		t.Fatalf("operator PUT branding: %d %v, want 403", code, body)
	}

	// The platform admin (holds every permission, including tenant.manage) can set it.
	logo := base64.StdEncoding.EncodeToString(testPNG(t))
	payload := `{"owner_name":"Municipalidad de Helvecia","logo":"` + logo + `","logo_content_type":"image/png"}`
	code, body = do(t, env.AdminToken, "PUT", brandingPath, payload)
	if code != http.StatusOK {
		t.Fatalf("admin PUT branding: %d %v", code, body)
	}
	if body["owner_name"] != "Municipalidad de Helvecia" || body["has_logo"] != true {
		t.Fatalf("updated branding = %v", body)
	}

	// The logo image itself downloads through the dedicated endpoint.
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+brandingPath+"/logo", nil)
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	logoBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get logo: %d", resp.StatusCode)
	}
	if !bytes.Equal(logoBody, testPNG(t)) {
		t.Errorf("logo bytes did not round-trip")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("logo content-type = %q, want image/png", ct)
	}

	// Reading it back now reflects the change for any tenant member.
	code, body = do(t, operatorToken, "GET", brandingPath, "")
	if code != http.StatusOK || body["owner_name"] != "Municipalidad de Helvecia" {
		t.Fatalf("get updated branding: %d %v", code, body)
	}

	// Another tenant's branding 404s rather than leaking its existence.
	otherTenant, err := env.Svc.CreateTenant(ctx, env.Admin, "otra-muni", "Otra Municipalidad")
	if err != nil {
		t.Fatal(err)
	}
	code, body = do(t, operatorToken, "GET", "/api/v1/tenants/"+otherTenant.ID.String()+"/branding", "")
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant branding read: %d %v, want 404", code, body)
	}

	// DELETE clears everything and is audited.
	code, body = do(t, env.AdminToken, "DELETE", brandingPath, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete branding: %d %v", code, body)
	}
	code, body = do(t, operatorToken, "GET", brandingPath, "")
	if code != http.StatusOK || body["owner_name"] != "" || body["has_logo"] != false {
		t.Fatalf("branding after delete: %d %v", code, body)
	}

	var updated, removed int
	if err := env.Pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE action = 'BRANDING_UPDATED'), count(*) FILTER (WHERE action = 'BRANDING_REMOVED')
		 FROM audit_log WHERE target_type = 'tenant' AND target_id = $1`, env.Demo.TenantID).
		Scan(&updated, &removed); err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Errorf("BRANDING_UPDATED audit rows = %d, want 1", updated)
	}
	if removed != 1 {
		t.Errorf("BRANDING_REMOVED audit rows = %d, want 1", removed)
	}

	// tenant.manage does not implicitly grant reading other tenants' data without it, but
	// this endpoint's own oversized-logo validation is exercised at the unit level
	// (internal/branding/service_test.go); here we only check the API surface rejects it too.
	tooBig := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, branding.MaxLogoBytes+1))
	code, body = do(t, env.AdminToken, "PUT", brandingPath, `{"logo":"`+tooBig+`","logo_content_type":"image/png"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("oversized logo: %d %v, want 400", code, body)
	}
}
