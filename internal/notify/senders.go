package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/netip"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Message is one notification ready to leave the platform.
type Message struct {
	ID         string
	Title      string
	Body       string
	Link       string
	Severity   string
	RuleID     string
	OccurredAt time.Time
	Test       bool
}

// Sender delivers a message to one destination of a channel (see Destinations).
type Sender interface {
	Send(ctx context.Context, m Message, dest string) error
}

// Deps carries what senders share: the HTTP client and the internal WAHA service settings.
type Deps struct {
	HTTP        *http.Client
	WahaBaseURL string
	WahaAPIKey  string
	// TelegramBaseURL defaults to https://api.telegram.org; tests point it at a fake.
	TelegramBaseURL string
	// DialContext dials SMTP servers; defaults to the guarded dialer.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

const (
	dialTimeout = 10 * time.Second
	sendTimeout = 30 * time.Second
	maxErrBody  = 300
)

// permanentError marks a failure that retrying cannot fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return &permanentError{err: err} }

// IsPermanent reports whether err should fail the delivery without further retries.
func IsPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// guardedDialer refuses link-local, unspecified and multicast targets after DNS resolution, so
// neither a hostname nor a redirect can reach the cloud metadata address.
func guardedDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: dialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip, err := netip.ParseAddr(host); err == nil && blockedAddr(ip) {
				return fmt.Errorf("connection to %s is not allowed", host)
			}
			return nil
		},
	}
}

// NewHTTPClient returns the client used for every outbound HTTP call: bounded by timeout, no
// proxy, no redirects, guarded dialer.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           guardedDialer().DialContext,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: timeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// NewSender builds the sender for a channel from its decrypted configuration.
func NewSender(t Type, cfg Config, sec Secrets, d Deps) (Sender, error) {
	if d.HTTP == nil {
		d.HTTP = NewHTTPClient(sendTimeout)
	}
	switch t {
	case TypeWebhook:
		return &webhookSender{cfg: cfg, sec: sec, http: d.HTTP}, nil
	case TypeTelegram:
		base := strings.TrimRight(d.TelegramBaseURL, "/")
		if base == "" {
			base = "https://api.telegram.org"
		}
		return &telegramSender{base: base, token: sec.BotToken, http: d.HTTP}, nil
	case TypeWhatsApp:
		if strings.TrimSpace(d.WahaBaseURL) == "" {
			return nil, errors.New("WhatsApp is not configured on this server (WAHA_BASE_URL is empty)")
		}
		return &whatsappSender{base: strings.TrimRight(d.WahaBaseURL, "/"), key: d.WahaAPIKey, session: cfg.Session, http: d.HTTP}, nil
	case TypeEmail:
		dial := d.DialContext
		if dial == nil {
			dial = guardedDialer().DialContext
		}
		return &emailSender{cfg: cfg, password: sec.SMTPPassword, dial: dial}, nil
	}
	return nil, fmt.Errorf("unsupported channel type %q", t)
}

// statusError turns a non-2xx response into an error. Client errors are permanent except the
// ones that signal a transient condition (408, 429) and any code in retryable.
func statusError(resp *http.Response, retryable ...int) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	msg := strings.Join(strings.Fields(string(b)), " ")
	err := fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	if msg == "" {
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	code := resp.StatusCode
	transient := code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
	for _, r := range retryable {
		transient = transient || code == r
	}
	if transient {
		return err
	}
	return permanent(err)
}

func doJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, body []byte, retryable ...int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenVMS-Notifier")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil
	}
	return statusError(resp, retryable...)
}

// --- webhook ---

type webhookSender struct {
	cfg  Config
	sec  Secrets
	http *http.Client
}

type webhookPayload struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Severity   string    `json:"severity"`
	Link       string    `json:"link,omitempty"`
	RuleID     string    `json:"rule_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Test       bool      `json:"test"`
}

// Send posts the JSON payload. With a signing secret it adds X-OpenVMS-Timestamp and
// X-OpenVMS-Signature: sha256=<hex HMAC-SHA256 of "<timestamp>.<body>">.
func (s *webhookSender) Send(ctx context.Context, m Message, _ string) error {
	body, err := json.Marshal(webhookPayload{
		ID: m.ID, Type: "openvms.notification", Title: m.Title, Body: m.Body, Severity: m.Severity,
		Link: m.Link, RuleID: m.RuleID, OccurredAt: m.OccurredAt.UTC(), Test: m.Test,
	})
	if err != nil {
		return permanent(err)
	}
	headers := map[string]string{}
	for k, v := range s.sec.Headers {
		headers[k] = v
	}
	if s.sec.SigningSecret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(s.sec.SigningSecret))
		mac.Write([]byte(ts + "." + string(body)))
		headers["X-OpenVMS-Timestamp"] = ts
		headers["X-OpenVMS-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	return doJSON(ctx, s.http, s.cfg.URL, headers, body)
}

// --- WhatsApp (WAHA) ---

type whatsappSender struct {
	base, key, session string
	http               *http.Client
}

// Send uses WAHA's POST /api/sendText. A 422 (session not working yet) is retried, since the
// operator may still be pairing the phone.
func (s *whatsappSender) Send(ctx context.Context, m Message, dest string) error {
	body, _ := json.Marshal(map[string]string{
		"session": s.session, "chatId": dest, "text": "*" + oneLine(m.Title) + "*\n" + m.Body,
	})
	headers := map[string]string{}
	if s.key != "" {
		headers["X-Api-Key"] = s.key
	}
	return doJSON(ctx, s.http, s.base+"/api/sendText", headers, body, http.StatusUnprocessableEntity)
}

// --- Telegram ---

type telegramSender struct {
	base, token string
	http        *http.Client
}

// Send calls the Bot API sendMessage. The token is part of the URL, so it is scrubbed from every
// returned error.
func (s *telegramSender) Send(ctx context.Context, m Message, dest string) error {
	body, _ := json.Marshal(map[string]any{"chat_id": dest, "text": oneLine(m.Title) + "\n" + m.Body})
	err := doJSON(ctx, s.http, s.base+"/bot"+s.token+"/sendMessage", nil, body)
	if err == nil {
		return nil
	}
	msg := strings.ReplaceAll(err.Error(), s.token, "***")
	if IsPermanent(err) {
		return permanent(errors.New(telegramDescription(msg)))
	}
	return errors.New(msg)
}

// telegramDescription extracts "description" from `HTTP 403: {"ok":false,"description":"..."}`.
func telegramDescription(msg string) string {
	i := strings.Index(msg, "{")
	if i < 0 {
		return msg
	}
	var v struct {
		Description string `json:"description"`
	}
	if json.Unmarshal([]byte(msg[i:]), &v) == nil && v.Description != "" {
		return msg[:i] + v.Description
	}
	return msg
}

// --- email ---

type emailSender struct {
	cfg      Config
	password string
	dial     func(ctx context.Context, network, addr string) (net.Conn, error)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// Send delivers one message to one recipient over SMTP with a bounded total time.
func (s *emailSender) Send(ctx context.Context, m Message, dest string) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	conn, err := s.dial(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(sendTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if s.cfg.TLS == "tls" {
		conn = tls.Client(conn, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return smtpError(err)
	}
	defer func() { _ = c.Close() }()
	if s.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return permanent(errors.New("server does not support STARTTLS"))
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpError(err)
		}
	}
	if s.cfg.Username != "" && s.password != "" {
		if s.cfg.TLS == "none" && !isLoopbackHost(s.cfg.Host) {
			return permanent(errors.New("refusing to send SMTP credentials over an unencrypted connection"))
		}
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.password, s.cfg.Host)); err != nil {
			return smtpError(err)
		}
	}
	from := s.cfg.From
	if err := c.Mail(bareAddress(from)); err != nil {
		return smtpError(err)
	}
	if err := c.Rcpt(dest); err != nil {
		return smtpError(err)
	}
	w, err := c.Data()
	if err != nil {
		return smtpError(err)
	}
	if _, err := w.Write(buildEmail(from, dest, m)); err != nil {
		return smtpError(err)
	}
	if err := w.Close(); err != nil {
		return smtpError(err)
	}
	return smtpError(c.Quit())
}

func bareAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

// smtpError marks 5xx replies as permanent; network errors and 4xx replies retry.
func smtpError(err error) error {
	if err == nil {
		return nil
	}
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 500 {
		return permanent(err)
	}
	return err
}

func buildEmail(from, to string, m Message) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from)
	h("To", to)
	h("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Title)))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	text := m.Body
	if m.Link != "" {
		text += "\n\n" + m.Link
	}
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	b.WriteString("\r\n")
	return b.Bytes()
}
