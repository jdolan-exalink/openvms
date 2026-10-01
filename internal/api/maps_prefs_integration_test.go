//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

const mapPrefsPath = "/api/v1/me/map-prefs"

func TestMapUserPrefsFeatureFlagDisabled(t *testing.T) {
	te := setupMapsTest(t, false)

	status, _, _ := te.request(http.MethodGet, mapPrefsPath, te.AdminToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get: expected 404 when maps flag disabled, got %d", status)
	}
	status, _, _ = te.request(http.MethodPut, mapPrefsPath, te.AdminToken, nil, gen.MapUserPrefs{})
	if status != http.StatusNotFound {
		t.Fatalf("put: expected 404 when maps flag disabled, got %d", status)
	}
}

func TestMapUserPrefsLifecycleAndIsolation(t *testing.T) {
	te := setupMapsTest(t, true)
	adminToken := te.AdminToken
	operatorToken := te.Demo.Tokens["operador"]

	// 1. A user who never saved anything gets an empty object, not an error.
	status, _, body := te.request(http.MethodGet, mapPrefsPath, adminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("first get: expected 200, got %d: %s", status, body)
	}
	prefs := decodeMapPrefs(t, body)
	if prefs.Layers != nil || prefs.Filters != nil || prefs.FocusMode != nil {
		t.Fatalf("expected empty prefs for a fresh user, got %s", body)
	}

	// 2. Saving echoes back exactly what was stored.
	save := gen.MapUserPrefs{
		Layers: &gen.MapLayerPreference{Cameras: boolPtr(true), Coverage: boolPtr(false)},
		Filters: &gen.MapFilters{
			Status: &[]gen.MapFiltersStatus{gen.ONLINE},
			Tags:   &[]string{"perimeter"},
		},
		FocusMode: focusModePtr(gen.MapUserPrefsFocusModeCurrentSite),
		HoverLive: boolPtr(true),
	}
	status, _, body = te.request(http.MethodPut, mapPrefsPath, adminToken, nil, save)
	if status != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", status, body)
	}
	stored := decodeMapPrefs(t, body)
	if stored.Layers == nil || stored.Layers.Cameras == nil || !*stored.Layers.Cameras {
		t.Fatalf("layers not echoed: %s", body)
	}
	if stored.FocusMode == nil || *stored.FocusMode != gen.MapUserPrefsFocusModeCurrentSite {
		t.Fatalf("focus_mode not echoed: %s", body)
	}

	// 3. A later save replaces the whole blob instead of merging it.
	status, _, body = te.request(http.MethodPut, mapPrefsPath, adminToken, nil, gen.MapUserPrefs{
		FocusMode: focusModePtr(gen.MapUserPrefsFocusModeNone),
	})
	if status != http.StatusOK {
		t.Fatalf("replace put: expected 200, got %d: %s", status, body)
	}
	status, _, body = te.request(http.MethodGet, mapPrefsPath, adminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("second get: expected 200, got %d: %s", status, body)
	}
	stored = decodeMapPrefs(t, body)
	if stored.Layers != nil || stored.Filters != nil {
		t.Fatalf("save must replace, not merge: %s", body)
	}
	if stored.FocusMode == nil || *stored.FocusMode != gen.MapUserPrefsFocusModeNone {
		t.Fatalf("replaced focus_mode missing: %s", body)
	}

	// 4. Preferences are per user: the operator never sees the admin's blob.
	status, _, body = te.request(http.MethodGet, mapPrefsPath, operatorToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("operator get: expected 200, got %d: %s", status, body)
	}
	stored = decodeMapPrefs(t, body)
	if stored.FocusMode != nil {
		t.Fatalf("operator must not inherit admin prefs: %s", body)
	}
	status, _, _ = te.request(http.MethodPut, mapPrefsPath, operatorToken, nil, gen.MapUserPrefs{
		Layers: &gen.MapLayerPreference{Heatmap: boolPtr(true)},
	})
	if status != http.StatusOK {
		t.Fatalf("operator put: expected 200, got %d", status)
	}
	status, _, body = te.request(http.MethodGet, mapPrefsPath, adminToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("admin get after operator write: expected 200, got %d", status)
	}
	stored = decodeMapPrefs(t, body)
	if stored.Layers != nil {
		t.Fatalf("operator write leaked into admin prefs: %s", body)
	}
}

func TestMapUserPrefsRejectsInvalidPayloads(t *testing.T) {
	te := setupMapsTest(t, true)

	// Wrong value type never reaches the service: strict decoding rejects it.
	status, _, _ := te.request(http.MethodPut, mapPrefsPath, te.AdminToken, nil, json.RawMessage(
		`{"layers":{"cameras":"yes"}}`,
	))
	if status != http.StatusBadRequest {
		t.Fatalf("non-boolean layer: expected 400, got %d", status)
	}

	// Unknown focus modes are rejected by the contract.
	status, _, _ = te.request(http.MethodPut, mapPrefsPath, te.AdminToken, nil, json.RawMessage(
		`{"focus_mode":"teleport"}`,
	))
	if status != http.StatusBadRequest {
		t.Fatalf("unknown focus mode: expected 400, got %d", status)
	}

	// A blob far beyond the size budget is refused instead of stored.
	huge := make([]string, 0, 4096)
	for i := 0; i < 4096; i++ {
		huge = append(huge, strings.Repeat("x", 32))
	}
	status, _, body := te.request(http.MethodPut, mapPrefsPath, te.AdminToken, nil, gen.MapUserPrefs{
		Filters: &gen.MapFilters{Tags: &huge},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("oversized blob: expected 400, got %d: %s", status, body)
	}
}

func boolPtr(v bool) *bool                                                { return &v }
func focusModePtr(v gen.MapUserPrefsFocusMode) *gen.MapUserPrefsFocusMode { return &v }

// decodeMapPrefs always decodes into a fresh value: json.Unmarshal leaves fields that are
// absent from the payload untouched, which would make a "replace, not merge" assertion lie.
func decodeMapPrefs(t *testing.T, body []byte) gen.MapUserPrefs {
	t.Helper()
	var p gen.MapUserPrefs
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
