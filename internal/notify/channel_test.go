package notify

import (
	"strings"
	"testing"
)

func TestCheckURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://hooks.example.com/x", true},
		{"http://10.0.0.5:8080/hook", true},
		{"http://localhost:9000/hook", true},
		{"ftp://example.com/x", false},
		{"javascript:alert(1)", false},
		{"https:///nohost", false},
		{"not a url", false},
		{"http://169.254.169.254/latest/meta-data", false},
		{"http://169.254.10.10/", false},
		{"http://[fe80::1]/", false},
		{"http://metadata.google.internal/computeMetadata/v1/", false},
		{"http://0.0.0.0/", false},
		{"https://user:pw@example.com/", false},
	}
	for _, c := range cases {
		err := CheckURL(c.url)
		if (err == nil) != c.ok {
			t.Errorf("CheckURL(%q) err = %v, want ok=%v", c.url, err, c.ok)
		}
	}
}

func TestNormalizeChatID(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"+54 9 11 5555-5555", "5491155555555@c.us", true},
		{"5491155555555", "5491155555555@c.us", true},
		{"5491155555555@c.us", "5491155555555@c.us", true},
		{"120363025246125486@g.us", "120363025246125486@g.us", true},
		{"54911-1234567890@g.us", "54911-1234567890@g.us", true},
		{"12345", "", false},
		{"abc", "", false},
		{"", "", false},
		{"123456789012345678901", "", false},
	}
	for _, c := range cases {
		got, err := NormalizeChatID(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("NormalizeChatID(%q) = %q, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name string
		typ  Type
		cfg  Config
		sec  Secrets
		err  string // substring; empty means valid
	}{
		{"webhook ok", TypeWebhook, Config{URL: "https://example.com/h"}, Secrets{}, ""},
		{"webhook missing url", TypeWebhook, Config{}, Secrets{}, "url"},
		{"webhook metadata", TypeWebhook, Config{URL: "http://169.254.169.254/"}, Secrets{}, "link-local"},
		{"webhook bad header", TypeWebhook, Config{URL: "https://example.com"}, Secrets{Headers: map[string]string{"Bad Name": "x"}}, "header"},
		{"webhook header injection", TypeWebhook, Config{URL: "https://example.com"}, Secrets{Headers: map[string]string{"X-A": "a\r\nX-B: b"}}, "header"},
		{"webhook reserved header", TypeWebhook, Config{URL: "https://example.com"}, Secrets{Headers: map[string]string{"X-OpenVMS-Signature": "x"}}, "header"},
		{"email ok", TypeEmail, Config{Host: "smtp.example.com", Port: 587, TLS: "starttls", From: "VMS <vms@example.com>", Recipients: []string{"a@example.com"}}, Secrets{}, ""},
		{"email no recipients", TypeEmail, Config{Host: "smtp.example.com", Port: 587, TLS: "starttls", From: "vms@example.com"}, Secrets{}, "recipient"},
		{"email bad recipient", TypeEmail, Config{Host: "h", Port: 25, TLS: "none", From: "vms@example.com", Recipients: []string{"nope"}}, Secrets{}, "recipient"},
		{"email bad port", TypeEmail, Config{Host: "h", Port: 0, TLS: "none", From: "vms@example.com", Recipients: []string{"a@example.com"}}, Secrets{}, "port"},
		{"email bad tls", TypeEmail, Config{Host: "h", Port: 25, TLS: "weird", From: "vms@example.com", Recipients: []string{"a@example.com"}}, Secrets{}, "tls"},
		{"email bad from", TypeEmail, Config{Host: "h", Port: 25, TLS: "none", From: "nope", Recipients: []string{"a@example.com"}}, Secrets{}, "from"},
		{"whatsapp ok", TypeWhatsApp, Config{Session: "default", Recipients: []string{"+5491155555555"}}, Secrets{}, ""},
		{"whatsapp bad session", TypeWhatsApp, Config{Session: "a b", Recipients: []string{"+5491155555555"}}, Secrets{}, "session"},
		{"whatsapp bad recipient", TypeWhatsApp, Config{Session: "default", Recipients: []string{"12"}}, Secrets{}, "recipient"},
		{"telegram ok", TypeTelegram, Config{ChatIDs: []string{"-1001234567890", "@canal"}}, Secrets{BotToken: "123:abc"}, ""},
		{"telegram no token", TypeTelegram, Config{ChatIDs: []string{"1"}}, Secrets{}, "bot_token"},
		{"telegram no chats", TypeTelegram, Config{}, Secrets{BotToken: "123:abc"}, "chat_ids"},
		{"telegram bad chat", TypeTelegram, Config{ChatIDs: []string{"a b"}}, Secrets{BotToken: "123:abc"}, "chat_ids"},
		{"unknown type", Type("sms"), Config{}, Secrets{}, "type"},
	}
	for _, c := range cases {
		_, err := ValidateConfig(c.typ, c.cfg, c.sec)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.err != "" && err == nil:
			t.Errorf("%s: expected error containing %q", c.name, c.err)
		case c.err != "" && !strings.Contains(strings.ToLower(err.Error()), c.err):
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.err)
		}
	}
}

func TestValidateConfigNormalizesWhatsAppRecipients(t *testing.T) {
	cfg, err := ValidateConfig(TypeWhatsApp, Config{Session: "default", Recipients: []string{"+54 9 11 5555-5555", "5491155555555"}}, Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Recipients) != 1 || cfg.Recipients[0] != "5491155555555@c.us" {
		t.Fatalf("recipients = %v, want one deduplicated chat id", cfg.Recipients)
	}
}

func TestDestinations(t *testing.T) {
	if got := Destinations(TypeWebhook, Config{URL: "https://x"}); len(got) != 1 || got[0] != "" {
		t.Errorf("webhook destinations = %v, want a single empty destination", got)
	}
	if got := Destinations(TypeEmail, Config{Recipients: []string{"a@x.com", "b@x.com"}}); len(got) != 2 {
		t.Errorf("email destinations = %v", got)
	}
	if got := Destinations(TypeTelegram, Config{ChatIDs: []string{"1", "2", "3"}}); len(got) != 3 {
		t.Errorf("telegram destinations = %v", got)
	}
	if got := Destinations(TypeWhatsApp, Config{Recipients: []string{"1@c.us"}}); len(got) != 1 || got[0] != "1@c.us" {
		t.Errorf("whatsapp destinations = %v", got)
	}
}

func TestSecretsSetAndMerge(t *testing.T) {
	old := Secrets{BotToken: "tok", Headers: map[string]string{"A": "1"}}
	if got := old.Set(); len(got) != 2 || got[0] != "bot_token" || got[1] != "headers" {
		t.Fatalf("Set() = %v", got)
	}
	merged := old.Merge(Secrets{SigningSecret: "s"}, nil)
	if merged.BotToken != "tok" || merged.SigningSecret != "s" || merged.Headers["A"] != "1" {
		t.Fatalf("merge kept/added wrong: %+v", merged)
	}
	cleared := old.Merge(Secrets{}, []string{"bot_token", "headers"})
	if len(cleared.Set()) != 0 {
		t.Fatalf("clear left %v", cleared.Set())
	}
}
