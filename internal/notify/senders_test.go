package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testMessage() Message {
	return Message{
		ID: "d1", Title: "Cámara offline", Body: "Cámara 'Acceso' está desconectado.",
		Severity: "critical", Link: "/cameras", RuleID: "r1",
		OccurredAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
}

func testDeps(srvURL string) Deps {
	return Deps{HTTP: NewHTTPClient(5 * time.Second), WahaBaseURL: srvURL, WahaAPIKey: "waha-key", TelegramBaseURL: srvURL}
}

func TestWebhookSenderSignsBody(t *testing.T) {
	var gotBody []byte
	var gotHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHdr = r.Header.Clone()
	}))
	defer srv.Close()

	s, err := NewSender(TypeWebhook, Config{URL: srv.URL + "/hook"}, Secrets{SigningSecret: "topsecret", Headers: map[string]string{"Authorization": "Bearer abc"}}, testDeps(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), testMessage(), ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotHdr.Get("Authorization") != "Bearer abc" {
		t.Errorf("custom header not sent: %v", gotHdr)
	}
	if gotHdr.Get("Content-Type") != "application/json" {
		t.Errorf("content type = %q", gotHdr.Get("Content-Type"))
	}
	ts := gotHdr.Get("X-OpenVMS-Timestamp")
	if ts == "" {
		t.Fatal("missing timestamp header")
	}
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write([]byte(ts + "." + string(gotBody)))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := gotHdr.Get("X-OpenVMS-Signature"); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if payload["title"] != "Cámara offline" || payload["severity"] != "critical" || payload["id"] != "d1" || payload["test"] != false {
		t.Errorf("payload = %v", payload)
	}
}

func TestWebhookSenderWithoutSecretSendsNoSignature(t *testing.T) {
	var gotHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { gotHdr = r.Header.Clone() }))
	defer srv.Close()
	s, _ := NewSender(TypeWebhook, Config{URL: srv.URL}, Secrets{}, testDeps(srv.URL))
	if err := s.Send(context.Background(), testMessage(), ""); err != nil {
		t.Fatal(err)
	}
	if gotHdr.Get("X-OpenVMS-Signature") != "" {
		t.Errorf("unexpected signature without a secret")
	}
}

func TestSendErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{{400, true}, {401, true}, {404, true}, {408, false}, {429, false}, {500, false}, {503, false}, {302, true}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://169.254.169.254/")
			w.WriteHeader(tc.status)
		}))
		s, _ := NewSender(TypeWebhook, Config{URL: srv.URL}, Secrets{}, testDeps(srv.URL))
		err := s.Send(context.Background(), testMessage(), "")
		srv.Close()
		if err == nil {
			t.Errorf("status %d: expected an error", tc.status)
			continue
		}
		if IsPermanent(err) != tc.permanent {
			t.Errorf("status %d: permanent = %v, want %v (%v)", tc.status, IsPermanent(err), tc.permanent, err)
		}
	}
}

func TestWebhookConnectionErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	s, _ := NewSender(TypeWebhook, Config{URL: url}, Secrets{}, testDeps(url))
	err := s.Send(context.Background(), testMessage(), "")
	if err == nil || IsPermanent(err) {
		t.Fatalf("connection refused should be a retryable error, got %v", err)
	}
}

func TestSafeDialerBlocksLinkLocal(t *testing.T) {
	client := NewHTTPClient(2 * time.Second)
	resp, err := client.Get("http://169.254.169.254/latest/meta-data")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("link-local dial should be refused by the dialer, got %v", err)
	}
}

func TestWhatsAppSenderPostsSendText(t *testing.T) {
	var got struct {
		path, key, ctype string
		body             map[string]string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.key, got.ctype = r.URL.Path, r.Header.Get("X-Api-Key"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s, err := NewSender(TypeWhatsApp, Config{Session: "default", Recipients: []string{"5491155555555@c.us"}}, Secrets{}, testDeps(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), testMessage(), "5491155555555@c.us"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got.path != "/api/sendText" || got.key != "waha-key" || got.ctype != "application/json" {
		t.Errorf("request = %+v", got)
	}
	if got.body["session"] != "default" || got.body["chatId"] != "5491155555555@c.us" || !strings.Contains(got.body["text"], "Cámara offline") || !strings.Contains(got.body["text"], "desconectado") {
		t.Errorf("body = %v", got.body)
	}
}

func TestWhatsAppSenderRequiresWaha(t *testing.T) {
	_, err := NewSender(TypeWhatsApp, Config{Session: "default"}, Secrets{}, Deps{HTTP: http.DefaultClient})
	if err == nil {
		t.Fatal("expected an error when WAHA is not configured")
	}
}

func TestTelegramSenderPostsSendMessageAndRedactsToken(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	s, err := NewSender(TypeTelegram, Config{ChatIDs: []string{"-100123"}}, Secrets{BotToken: "123:SECRETTOKEN"}, testDeps(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), testMessage(), "-100123"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if path != "/bot123:SECRETTOKEN/sendMessage" {
		t.Errorf("path = %q", path)
	}
	if body["chat_id"] != "-100123" || !strings.Contains(fmt.Sprint(body["text"]), "Cámara offline") {
		t.Errorf("body = %v", body)
	}
	srv.Close()

	// The bot token is part of the URL: it must not leak through transport errors.
	err = s.Send(context.Background(), testMessage(), "-100123")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Errorf("error leaks the bot token: %v", err)
	}
}

func TestTelegramSenderAPIErrorIsPermanentWhenForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"description":"bot was blocked by the user"}`))
	}))
	defer srv.Close()
	s, _ := NewSender(TypeTelegram, Config{ChatIDs: []string{"1"}}, Secrets{BotToken: "t"}, testDeps(srv.URL))
	err := s.Send(context.Background(), testMessage(), "1")
	if err == nil || !IsPermanent(err) || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err = %v", err)
	}
}

// fakeSMTP is a minimal SMTP server that records the envelope and the message.
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	from string
	rcpt []string
	data string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			w("250 fake")
		case strings.HasPrefix(up, "MAIL FROM:"):
			f.mu.Lock()
			f.from = line[len("MAIL FROM:"):]
			f.mu.Unlock()
			w("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			f.mu.Lock()
			f.rcpt = append(f.rcpt, line[len("RCPT TO:"):])
			f.mu.Unlock()
			w("250 ok")
		case up == "DATA":
			w("354 go")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				sb.WriteString(l)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 queued")
		case up == "QUIT":
			w("221 bye")
			return
		default:
			w("502 unsupported")
		}
	}
}

func TestEmailSenderDeliversMessage(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := Config{Host: "127.0.0.1", Port: f.port(), TLS: "none", From: "OpenVMS <vms@example.com>", Recipients: []string{"ops@example.com"}}
	s, err := NewSender(TypeEmail, cfg, Secrets{}, testDeps(""))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), testMessage(), "ops@example.com"); err != nil {
		t.Fatalf("send: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(f.from, "vms@example.com") || len(f.rcpt) != 1 || !strings.Contains(f.rcpt[0], "ops@example.com") {
		t.Errorf("envelope from=%q rcpt=%v", f.from, f.rcpt)
	}
	for _, want := range []string{"To: ops@example.com", "From: ", "Subject: ", "Content-Type: text/plain; charset=utf-8"} {
		if !strings.Contains(f.data, want) {
			t.Errorf("message lacks %q:\n%s", want, f.data)
		}
	}
}

func TestEmailSenderFailsWhenAuthCannotBeUsed(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := Config{Host: "127.0.0.1", Port: f.port(), TLS: "none", Username: "u", From: "vms@example.com", Recipients: []string{"ops@example.com"}}
	s, _ := NewSender(TypeEmail, cfg, Secrets{SMTPPassword: "pw"}, testDeps(""))
	if err := s.Send(context.Background(), testMessage(), "ops@example.com"); err == nil {
		t.Fatal("expected an error: the fake server offers no AUTH")
	}
}

func TestEmailSubjectCannotInjectHeaders(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := Config{Host: "127.0.0.1", Port: f.port(), TLS: "none", From: "vms@example.com", Recipients: []string{"ops@example.com"}}
	s, _ := NewSender(TypeEmail, cfg, Secrets{}, testDeps(""))
	m := testMessage()
	m.Title = "hola\r\nBcc: evil@example.com"
	if err := s.Send(context.Background(), m, "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(f.data, "\r\nBcc:") {
		t.Errorf("header injection succeeded:\n%s", f.data)
	}
}
