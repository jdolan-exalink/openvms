//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestNotificationChannelsCRUDSecretsAndRBAC(t *testing.T) {
	te, token := setupRulesTest(t)
	operator := te.Demo.Tokens["operador"]

	// Operators without notifications.manage get 403 on every verb.
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/notification-channels"},
		{"POST", "/api/v1/notification-channels"},
	} {
		if code, body := te.request(c.method, c.path, operator, map[string]any{"name": "x", "type": "webhook"}); code != 403 {
			t.Fatalf("%s %s as operator = %d, want 403: %s", c.method, c.path, code, body)
		}
	}

	// Validation: link-local webhook targets and unknown types are 400.
	for _, bad := range []map[string]any{
		{"name": "meta", "type": "webhook", "config": map[string]any{"url": "http://169.254.169.254/latest"}},
		{"name": "ftp", "type": "webhook", "config": map[string]any{"url": "ftp://example.com"}},
		{"name": "tg", "type": "telegram", "config": map[string]any{"chat_ids": []string{"1"}}},
	} {
		if code, body := te.request("POST", "/api/v1/notification-channels", token, bad); code != 400 {
			t.Fatalf("create %v = %d, want 400: %s", bad, code, body)
		}
	}

	create := map[string]any{
		"name": "Webhook guardia", "type": "webhook",
		"config":  map[string]any{"url": "https://hooks.example.com/vms"},
		"secrets": map[string]any{"signing_secret": "topsecret-value", "headers": map[string]string{"Authorization": "Bearer sup3r-token"}},
	}
	code, body := te.request("POST", "/api/v1/notification-channels", token, create)
	if code != 201 {
		t.Fatalf("create = %d, want 201: %s", code, body)
	}
	assertNoSecrets(t, "create response", body)
	var ch gen.NotificationChannel
	if err := json.Unmarshal(body, &ch); err != nil {
		t.Fatal(err)
	}
	if !ch.Enabled || ch.Config.Url == nil || *ch.Config.Url != "https://hooks.example.com/vms" {
		t.Fatalf("unexpected channel: %+v", ch)
	}
	if strings.Join(ch.SecretsSet, ",") != "headers,signing_secret" {
		t.Fatalf("secrets_set = %v", ch.SecretsSet)
	}

	// Duplicate names conflict.
	if code, body := te.request("POST", "/api/v1/notification-channels", token, create); code != 409 {
		t.Fatalf("duplicate create = %d, want 409: %s", code, body)
	}

	path := "/api/v1/notification-channels/" + ch.Id.String()
	for _, p := range []string{"/api/v1/notification-channels", path} {
		code, body := te.request("GET", p, token, nil)
		if code != 200 {
			t.Fatalf("GET %s = %d: %s", p, code, body)
		}
		assertNoSecrets(t, "GET "+p, body)
	}

	// Stored blob is sealed: no plaintext in the database.
	var sealed []byte
	if err := te.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT secrets_sealed FROM notification_channels WHERE id = $1`, ch.Id).Scan(&sealed)
	}); err != nil {
		t.Fatal(err)
	}
	if len(sealed) == 0 || bytes.Contains(sealed, []byte("topsecret-value")) || bytes.Contains(sealed, []byte("sup3r-token")) {
		t.Fatalf("secrets are not sealed at rest: %q", sealed)
	}

	// Updating the name keeps the secrets; clearing one removes only that one.
	code, body = te.request("PATCH", path, token, map[string]any{"name": "Webhook guardia 2", "enabled": false})
	if code != 200 {
		t.Fatalf("patch = %d: %s", code, body)
	}
	assertNoSecrets(t, "patch response", body)
	_ = json.Unmarshal(body, &ch)
	if ch.Name != "Webhook guardia 2" || ch.Enabled || strings.Join(ch.SecretsSet, ",") != "headers,signing_secret" {
		t.Fatalf("patch result: %+v", ch)
	}
	code, body = te.request("PATCH", path, token, map[string]any{"clear_secrets": []string{"headers"}})
	if code != 200 {
		t.Fatalf("clear = %d: %s", code, body)
	}
	_ = json.Unmarshal(body, &ch)
	if strings.Join(ch.SecretsSet, ",") != "signing_secret" {
		t.Fatalf("after clear secrets_set = %v", ch.SecretsSet)
	}

	// Rules can select the channel; unknown ids are rejected; deleting strips it from rules.
	if code, body := te.request("POST", "/api/v1/rules", token, map[string]any{
		"name": "r", "trigger_type": "event",
		"actions": map[string]any{"channel_ids": []string{"00000000-0000-0000-0000-000000000001"}},
	}); code != 400 {
		t.Fatalf("rule with unknown channel = %d, want 400: %s", code, body)
	}
	code, body = te.request("POST", "/api/v1/rules", token, map[string]any{
		"name": "r", "trigger_type": "event",
		"actions": map[string]any{"notify_in_app": true, "channel_ids": []string{ch.Id.String()}},
	})
	if code != 201 {
		t.Fatalf("rule with channel = %d: %s", code, body)
	}
	var rule gen.Rule
	_ = json.Unmarshal(body, &rule)
	if rule.Actions.ChannelIds == nil || len(*rule.Actions.ChannelIds) != 1 {
		t.Fatalf("channel_ids not round-tripped: %+v", rule.Actions)
	}

	// Audit rows exist and hold no secret values.
	var details string
	if err := te.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT string_agg(details::text, ' ') FROM audit_log WHERE target_type = 'notification_channel'`).Scan(&details)
	}); err != nil {
		t.Fatal(err)
	}
	if details == "" || strings.Contains(details, "topsecret-value") || strings.Contains(details, "sup3r-token") {
		t.Fatalf("audit details: %q", details)
	}

	if code, body := te.request("DELETE", path, operator, nil); code != 403 {
		t.Fatalf("operator delete = %d, want 403: %s", code, body)
	}
	if code, body := te.request("DELETE", path, token, nil); code != 204 {
		t.Fatalf("delete = %d: %s", code, body)
	}
	if code, _ := te.request("GET", path, token, nil); code != 404 {
		t.Fatalf("get deleted = %d, want 404", code)
	}
	code, body = te.request("GET", "/api/v1/rules/"+rule.Id.String(), token, nil)
	if code != 200 {
		t.Fatalf("get rule = %d: %s", code, body)
	}
	var after gen.Rule
	_ = json.Unmarshal(body, &after)
	if after.Actions.ChannelIds != nil && len(*after.Actions.ChannelIds) != 0 {
		t.Fatalf("deleted channel still linked: %+v", after.Actions)
	}
}

func assertNoSecrets(t *testing.T, what string, body []byte) {
	t.Helper()
	for _, s := range []string{"topsecret-value", "sup3r-token", "Bearer"} {
		if bytes.Contains(body, []byte(s)) {
			t.Fatalf("%s leaks %q: %s", what, s, body)
		}
	}
}

func TestNotificationChannelTestSendAndDeliveries(t *testing.T) {
	te, token := setupRulesTest(t)
	operator := te.Demo.Tokens["operador"]

	var hits atomic.Int32
	var gotSig atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotSig.Store(r.Header.Get("X-OpenVMS-Signature"))
	}))
	defer srv.Close()

	code, body := te.request("POST", "/api/v1/notification-channels", token, map[string]any{
		"name": "Local", "type": "webhook", "config": map[string]any{"url": srv.URL},
		"secrets": map[string]any{"signing_secret": "topsecret-value"},
	})
	if code != 201 {
		t.Fatalf("create = %d: %s", code, body)
	}
	var ch gen.NotificationChannel
	_ = json.Unmarshal(body, &ch)
	path := "/api/v1/notification-channels/" + ch.Id.String() + "/test"

	if code, body := te.request("POST", path, operator, nil); code != 403 {
		t.Fatalf("operator test = %d, want 403: %s", code, body)
	}
	code, body = te.request("POST", path, token, nil)
	if code != 200 {
		t.Fatalf("test = %d: %s", code, body)
	}
	var res gen.NotificationChannelTestResult
	_ = json.Unmarshal(body, &res)
	if len(res.Results) != 1 || !res.Results[0].Ok || hits.Load() != 1 {
		t.Fatalf("results = %+v hits = %d", res, hits.Load())
	}
	if sig, _ := gotSig.Load().(string); !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("test message was not signed: %q", sig)
	}

	// A failing receiver is reported per destination instead of failing the request.
	srv.Close()
	code, body = te.request("POST", path, token, nil)
	_ = json.Unmarshal(body, &res)
	if code != 200 || len(res.Results) != 1 || res.Results[0].Ok || res.Results[0].Error == nil {
		t.Fatalf("failing test = %d %s", code, body)
	}

	// Deliveries: enqueue through the outbox and list, filtered by channel.
	if err := te.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := notify.Enqueue(context.Background(), tx, notify.EnqueueInput{
			TenantID: te.Demo.TenantID, ChannelIDs: []uuid.UUID{ch.Id}, Title: "t", Body: "b", Severity: "info",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if code, body := te.request("GET", "/api/v1/notification-deliveries", operator, nil); code != 403 {
		t.Fatalf("operator deliveries = %d, want 403: %s", code, body)
	}
	code, body = te.request("GET", "/api/v1/notification-deliveries?channel_id="+ch.Id.String(), token, nil)
	var list gen.NotificationDeliveryList
	_ = json.Unmarshal(body, &list)
	if code != 200 || len(list.Items) != 1 || list.Items[0].Status != gen.NotificationDeliveryStatusPending || list.Items[0].ChannelName != "Local" {
		t.Fatalf("deliveries = %d %s", code, body)
	}
	code, body = te.request("GET", "/api/v1/notification-deliveries?channel_id="+uuid.NewString(), token, nil)
	_ = json.Unmarshal(body, &list)
	if code != 200 || len(list.Items) != 0 {
		t.Fatalf("other channel deliveries = %d %s", code, body)
	}
}

func TestWhatsAppPairingEndpoints(t *testing.T) {
	var status atomic.Value
	status.Store("SCAN_QR_CODE")
	waha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "waha-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/default":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "default", "status": status.Load()})
		case r.Method == http.MethodGet && r.URL.Path == "/api/default/auth/qr":
			if status.Load() != "SCAN_QR_CODE" {
				http.Error(w, "no qr", http.StatusUnprocessableEntity)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"mimetype": "image/png", "data": "QVJD"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/default/start":
			status.Store("STARTING")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "default", "status": "STARTING"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer waha.Close()
	te, token := setupRulesTestWith(t, notify.Deps{WahaBaseURL: waha.URL, WahaAPIKey: "waha-key"})
	operator := te.Demo.Tokens["operador"]

	code, body := te.request("POST", "/api/v1/notification-channels", token, map[string]any{
		"name": "WhatsApp guardia", "type": "whatsapp",
		"config": map[string]any{"session": "default", "recipients": []string{"+54 9 11 5555-5555"}},
	})
	if code != 201 {
		t.Fatalf("create whatsapp = %d: %s", code, body)
	}
	var wa gen.NotificationChannel
	_ = json.Unmarshal(body, &wa)
	if wa.Config.Recipients == nil || (*wa.Config.Recipients)[0] != "5491155555555@c.us" {
		t.Fatalf("recipients not normalized: %+v", wa.Config)
	}
	base := "/api/v1/notification-channels/" + wa.Id.String() + "/whatsapp"

	if code, body := te.request("GET", base+"/session", operator, nil); code != 403 {
		t.Fatalf("operator session = %d, want 403: %s", code, body)
	}
	code, body = te.request("GET", base+"/session", token, nil)
	var sess gen.WhatsAppSession
	_ = json.Unmarshal(body, &sess)
	if code != 200 || sess.Status != "SCAN_QR_CODE" || sess.Name != "default" {
		t.Fatalf("session = %d %s", code, body)
	}
	code, body = te.request("GET", base+"/qr", token, nil)
	var qr gen.WhatsAppQR
	_ = json.Unmarshal(body, &qr)
	if code != 200 || qr.Mimetype != "image/png" || qr.Data != "QVJD" {
		t.Fatalf("qr = %d %s", code, body)
	}
	status.Store("WORKING")
	if code, body := te.request("GET", base+"/qr", token, nil); code != 409 {
		t.Fatalf("qr while working = %d, want 409: %s", code, body)
	}
	code, body = te.request("POST", base+"/session/start", token, nil)
	_ = json.Unmarshal(body, &sess)
	if code != 200 || sess.Status != "STARTING" {
		t.Fatalf("start = %d %s", code, body)
	}

	// Non-WhatsApp channels are rejected; an unreachable WAHA is a 502.
	code, body = te.request("POST", "/api/v1/notification-channels", token, map[string]any{
		"name": "hook", "type": "webhook", "config": map[string]any{"url": "https://example.com/h"},
	})
	var hook gen.NotificationChannel
	_ = json.Unmarshal(body, &hook)
	if code != 201 {
		t.Fatalf("create webhook = %d: %s", code, body)
	}
	if code, body := te.request("GET", "/api/v1/notification-channels/"+hook.Id.String()+"/whatsapp/session", token, nil); code != 400 {
		t.Fatalf("session of a webhook channel = %d, want 400: %s", code, body)
	}
	waha.Close()
	if code, body := te.request("GET", base+"/session", token, nil); code != 502 {
		t.Fatalf("session with WAHA down = %d, want 502: %s", code, body)
	}
}
