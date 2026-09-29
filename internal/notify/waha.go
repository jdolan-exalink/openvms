package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ErrQRUnavailable means WAHA has no QR to show: the session is not waiting for a scan.
var ErrQRUnavailable = errors.New("no QR code is available; the session is not waiting to be paired")

// WahaError wraps failures to reach or talk to the WAHA service.
type WahaError struct{ Err error }

func (e *WahaError) Error() string { return "WhatsApp service unavailable: " + e.Err.Error() }
func (e *WahaError) Unwrap() error { return e.Err }

func isWahaUnavailable(err error) bool {
	var we *WahaError
	return errors.As(err, &we)
}

// WhatsAppSession is the pairing state of a WAHA session. Status is WAHA's own value (STOPPED,
// STARTING, SCAN_QR_CODE, WORKING, FAILED) or NOT_FOUND when the session does not exist yet.
type WhatsAppSession struct {
	Name   string
	Status string
	Phone  string
}

type WhatsAppQR struct {
	Mimetype string
	Data     string // base64
}

// wahaClient talks to WAHA's HTTP API (https://waha.devlike.pro): GET /api/sessions/{s},
// POST /api/sessions/{s}/start, POST /api/sessions and GET /api/{s}/auth/qr, all with X-Api-Key.
type wahaClient struct {
	base, key string
	http      *http.Client
}

func (c *wahaClient) do(ctx context.Context, method, path string, body any, accept string) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if c.key != "" {
		req.Header.Set("X-Api-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, &WahaError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, &WahaError{Err: err}
	}
	return resp.StatusCode, b, nil
}

func parseSession(b []byte) (WhatsAppSession, error) {
	var v struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Me     *struct {
			ID string `json:"id"`
		} `json:"me"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return WhatsAppSession{}, &WahaError{Err: fmt.Errorf("unexpected response: %w", err)}
	}
	s := WhatsAppSession{Name: v.Name, Status: v.Status}
	if v.Me != nil {
		s.Phone, _, _ = strings.Cut(v.Me.ID, "@")
	}
	return s, nil
}

func wahaStatusErr(code int, b []byte) error {
	msg := strings.Join(strings.Fields(string(b)), " ")
	return &WahaError{Err: fmt.Errorf("HTTP %d: %s", code, msg[:min(len(msg), 200)])}
}

func (c *wahaClient) Session(ctx context.Context, name string) (WhatsAppSession, error) {
	code, b, err := c.do(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(name), nil, "")
	if err != nil {
		return WhatsAppSession{}, err
	}
	switch {
	case code == http.StatusNotFound:
		return WhatsAppSession{Name: name, Status: "NOT_FOUND"}, nil
	case code >= 300:
		return WhatsAppSession{}, wahaStatusErr(code, b)
	}
	return parseSession(b)
}

// Start starts the session, creating it first when WAHA does not know it.
func (c *wahaClient) Start(ctx context.Context, name string) (WhatsAppSession, error) {
	code, b, err := c.do(ctx, http.MethodPost, "/api/sessions/"+url.PathEscape(name)+"/start", nil, "")
	if err != nil {
		return WhatsAppSession{}, err
	}
	if code == http.StatusNotFound {
		code, b, err = c.do(ctx, http.MethodPost, "/api/sessions", map[string]any{"name": name}, "")
		if err != nil {
			return WhatsAppSession{}, err
		}
	}
	if code >= 300 {
		return WhatsAppSession{}, wahaStatusErr(code, b)
	}
	s, err := parseSession(b)
	if err != nil || s.Name == "" {
		return c.Session(ctx, name)
	}
	return s, nil
}

func (c *wahaClient) QR(ctx context.Context, name string) (WhatsAppQR, error) {
	code, b, err := c.do(ctx, http.MethodGet, "/api/"+url.PathEscape(name)+"/auth/qr", nil, "application/json")
	if err != nil {
		return WhatsAppQR{}, err
	}
	if code == http.StatusNotFound || code == http.StatusUnprocessableEntity {
		return WhatsAppQR{}, ErrQRUnavailable
	}
	if code >= 300 {
		return WhatsAppQR{}, wahaStatusErr(code, b)
	}
	var v struct {
		Mimetype string `json:"mimetype"`
		Data     string `json:"data"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Data == "" {
		return WhatsAppQR{}, ErrQRUnavailable
	}
	return WhatsAppQR{Mimetype: v.Mimetype, Data: v.Data}, nil
}

// waha returns the client and session name for a WhatsApp channel of the actor's tenant.
func (s *Service) waha(ctx context.Context, actor authz.Actor, id uuid.UUID) (*wahaClient, string, error) {
	var row db.NotificationChannel
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		var err error
		if row, err = q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: id, TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if Type(row.Type) != TypeWhatsApp {
		return nil, "", &ValidationError{Msg: "the channel is not a WhatsApp channel"}
	}
	if strings.TrimSpace(s.Deps.WahaBaseURL) == "" {
		return nil, "", &ValidationError{Msg: "WhatsApp is not configured on this server (WAHA_BASE_URL is empty)"}
	}
	return &wahaClient{base: strings.TrimRight(s.Deps.WahaBaseURL, "/"), key: s.Deps.WahaAPIKey, http: s.Deps.HTTP}, decodeConfig(row).Session, nil
}

// WhatsAppSession reports the pairing state of the channel's WAHA session.
func (s *Service) WhatsAppSession(ctx context.Context, actor authz.Actor, id uuid.UUID) (WhatsAppSession, error) {
	c, session, err := s.waha(ctx, actor, id)
	if err != nil {
		return WhatsAppSession{}, err
	}
	return c.Session(ctx, session)
}

// WhatsAppStart starts (creating if needed) the channel's WAHA session.
func (s *Service) WhatsAppStart(ctx context.Context, actor authz.Actor, id uuid.UUID) (WhatsAppSession, error) {
	c, session, err := s.waha(ctx, actor, id)
	if err != nil {
		return WhatsAppSession{}, err
	}
	return c.Start(ctx, session)
}

// WhatsAppQR returns the pairing QR image (base64) while the session waits to be scanned.
func (s *Service) WhatsAppQR(ctx context.Context, actor authz.Actor, id uuid.UUID) (WhatsAppQR, error) {
	c, session, err := s.waha(ctx, actor, id)
	if err != nil {
		return WhatsAppQR{}, err
	}
	return c.QR(ctx, session)
}
