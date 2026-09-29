// Package notify delivers rule notifications to external channels (webhook, email, WhatsApp
// through WAHA, Telegram). Rules enqueue deliveries into a database outbox; a worker sends them.
package notify

import (
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type Type string

const (
	TypeWebhook  Type = "webhook"
	TypeEmail    Type = "email"
	TypeWhatsApp Type = "whatsapp"
	TypeTelegram Type = "telegram"
)

// ValidationError is a request rejected before touching the store or the network.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Config is the non-secret, type specific part of a channel. Only the fields of its type are used.
type Config struct {
	URL        string   `json:"url,omitempty"`
	Host       string   `json:"host,omitempty"`
	Port       int      `json:"port,omitempty"`
	TLS        string   `json:"tls,omitempty"`
	Username   string   `json:"username,omitempty"`
	From       string   `json:"from,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
	Session    string   `json:"session,omitempty"`
	ChatIDs    []string `json:"chat_ids,omitempty"`
}

// Secrets are the write-only values of a channel, stored as one sealed JSON blob.
type Secrets struct {
	SigningSecret string            `json:"signing_secret,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	SMTPPassword  string            `json:"smtp_password,omitempty"`
	BotToken      string            `json:"bot_token,omitempty"`
}

const (
	maxDestinations = 20
	maxHeaders      = 20
)

var (
	headerName    = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)
	sessionName   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	groupChatID   = regexp.MustCompile(`^\d+(-\d+)?@g\.us$`)
	userChatID    = regexp.MustCompile(`^\d{8,15}@c\.us$`)
	telegramChat  = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z0-9_]{4,64})$`)
	reservedNames = []string{"host", "content-length", "content-type", "x-openvms-signature", "x-openvms-timestamp"}
	// metadataHosts are cloud metadata names blocked in addition to the link-local IP ranges.
	metadataHosts = []string{"metadata.google.internal", "metadata"}
)

// blockedAddr reports whether an outbound connection to addr is refused: link-local
// (169.254.0.0/16 including the cloud metadata address, fe80::/10), unspecified and multicast.
// Private LAN and loopback addresses stay allowed because on-premise receivers are the norm.
func blockedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast()
}

// CheckURL accepts only http(s) URLs with a host and no embedded credentials, and refuses
// link-local and metadata destinations.
func CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return &ValidationError{Msg: "url must be an absolute http or https URL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &ValidationError{Msg: "url must use http or https"}
	}
	if u.User != nil {
		return &ValidationError{Msg: "url must not embed credentials; use custom headers"}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return &ValidationError{Msg: "url must be an absolute http or https URL"}
	}
	if slices.Contains(metadataHosts, host) {
		return &ValidationError{Msg: "url points to a metadata host, which is not allowed"}
	}
	if addr, err := netip.ParseAddr(host); err == nil && blockedAddr(addr) {
		return &ValidationError{Msg: "url points to a link-local, unspecified or multicast address, which is not allowed"}
	}
	return nil
}

// NormalizeChatID turns a phone number or WhatsApp id into a WAHA chatId.
func NormalizeChatID(in string) (string, error) {
	in = strings.TrimSpace(in)
	if groupChatID.MatchString(in) || userChatID.MatchString(in) {
		return in, nil
	}
	num := strings.TrimSuffix(in, "@c.us")
	var digits strings.Builder
	for _, r := range num {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return "", fmt.Errorf("invalid WhatsApp recipient %q", in)
		}
	}
	if n := digits.Len(); n < 8 || n > 15 {
		return "", fmt.Errorf("invalid WhatsApp recipient %q: expected 8 to 15 digits", in)
	}
	return digits.String() + "@c.us", nil
}

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// ValidateConfig checks a channel's config and secrets for its type and returns the normalized
// config to store. sec must be the effective secrets (existing values merged with the update).
func ValidateConfig(t Type, cfg Config, sec Secrets) (Config, error) {
	switch t {
	case TypeWebhook:
		cfg = Config{URL: strings.TrimSpace(cfg.URL)}
		if cfg.URL == "" {
			return cfg, invalid("url is required")
		}
		if err := CheckURL(cfg.URL); err != nil {
			return cfg, err
		}
		if len(sec.Headers) > maxHeaders {
			return cfg, invalid("at most %d custom headers are allowed", maxHeaders)
		}
		for k, v := range sec.Headers {
			if !headerName.MatchString(k) || slices.Contains(reservedNames, strings.ToLower(k)) {
				return cfg, invalid("invalid or reserved header name %q", k)
			}
			if strings.ContainsAny(v, "\r\n\x00") {
				return cfg, invalid("invalid value for header %q", k)
			}
		}
	case TypeEmail:
		cfg.Recipients = cleanList(cfg.Recipients)
		cfg = Config{Host: strings.TrimSpace(cfg.Host), Port: cfg.Port, TLS: cfg.TLS, Username: cfg.Username, From: strings.TrimSpace(cfg.From), Recipients: cfg.Recipients}
		if cfg.Host == "" || strings.ContainsAny(cfg.Host, " /\r\n") {
			return cfg, invalid("host is required")
		}
		if cfg.Port < 1 || cfg.Port > 65535 {
			return cfg, invalid("port must be between 1 and 65535")
		}
		if !slices.Contains([]string{"none", "starttls", "tls"}, cfg.TLS) {
			return cfg, invalid("tls must be none, starttls or tls")
		}
		if _, err := mail.ParseAddress(cfg.From); err != nil || strings.ContainsAny(cfg.From, "\r\n") {
			return cfg, invalid("from must be a valid email address")
		}
		if len(cfg.Recipients) == 0 || len(cfg.Recipients) > maxDestinations {
			return cfg, invalid("between 1 and %d recipients are required", maxDestinations)
		}
		for _, r := range cfg.Recipients {
			if a, err := mail.ParseAddress(r); err != nil || a.Address != r {
				return cfg, invalid("invalid recipient %q: use a bare email address", r)
			}
		}
	case TypeWhatsApp:
		cfg = Config{Session: strings.TrimSpace(cfg.Session), Recipients: cfg.Recipients}
		if !sessionName.MatchString(cfg.Session) {
			return cfg, invalid("session must be 1 to 64 letters, digits, dashes or underscores")
		}
		var norm []string
		for _, r := range cfg.Recipients {
			if strings.TrimSpace(r) == "" {
				continue
			}
			id, err := NormalizeChatID(r)
			if err != nil {
				return cfg, invalid("invalid recipient: %v", err)
			}
			if !slices.Contains(norm, id) {
				norm = append(norm, id)
			}
		}
		if len(norm) == 0 || len(norm) > maxDestinations {
			return cfg, invalid("between 1 and %d recipients are required", maxDestinations)
		}
		cfg.Recipients = norm
	case TypeTelegram:
		cfg = Config{ChatIDs: cleanList(cfg.ChatIDs)}
		if sec.BotToken == "" {
			return cfg, invalid("bot_token is required")
		}
		if len(cfg.ChatIDs) == 0 || len(cfg.ChatIDs) > maxDestinations {
			return cfg, invalid("between 1 and %d chat_ids are required", maxDestinations)
		}
		for _, c := range cfg.ChatIDs {
			if !telegramChat.MatchString(c) {
				return cfg, invalid("invalid chat_ids entry %q", c)
			}
		}
	default:
		return cfg, invalid("invalid channel type %q", t)
	}
	return cfg, nil
}

// Destinations lists what one message fans out to. Each destination is its own delivery, so a
// retry never resends to destinations that already succeeded. Webhooks have one empty destination.
func Destinations(t Type, cfg Config) []string {
	switch t {
	case TypeEmail, TypeWhatsApp:
		return slices.Clone(cfg.Recipients)
	case TypeTelegram:
		return slices.Clone(cfg.ChatIDs)
	case TypeWebhook:
		return []string{""}
	}
	return nil
}

// Set lists the names of the secrets that hold a value.
func (s Secrets) Set() []string {
	var out []string
	if s.SigningSecret != "" {
		out = append(out, "signing_secret")
	}
	if len(s.Headers) > 0 {
		out = append(out, "headers")
	}
	if s.SMTPPassword != "" {
		out = append(out, "smtp_password")
	}
	if s.BotToken != "" {
		out = append(out, "bot_token")
	}
	slices.Sort(out)
	return out
}

// Merge overlays the non-empty values of in, then drops the secrets named in clear.
func (s Secrets) Merge(in Secrets, drop []string) Secrets {
	out := s
	if in.SigningSecret != "" {
		out.SigningSecret = in.SigningSecret
	}
	if len(in.Headers) > 0 {
		out.Headers = in.Headers
	}
	if in.SMTPPassword != "" {
		out.SMTPPassword = in.SMTPPassword
	}
	if in.BotToken != "" {
		out.BotToken = in.BotToken
	}
	for _, name := range drop {
		switch name {
		case "signing_secret":
			out.SigningSecret = ""
		case "headers":
			out.Headers = nil
		case "smtp_password":
			out.SMTPPassword = ""
		case "bot_token":
			out.BotToken = ""
		}
	}
	return out
}
