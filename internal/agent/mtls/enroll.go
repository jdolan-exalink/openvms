package mtls

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrTokenRejected is returned when the API refuses the enrollment token: it is unknown, was
// already used or has expired (the API answers all three the same way).
var ErrTokenRejected = errors.New("the API rejected the enrollment token: it is invalid, already used or expired (tokens are single use and last 15 minutes); create a new token for this server and set OPENVMS_ENROLL_TOKEN again")

const (
	enrollPath       = "/api/v1/agent/enroll"
	maxEnrollReply   = 1 << 20
	enrollTimeout    = 30 * time.Second
	maxErrorBodyEcho = 256
)

// NewAPIClient builds the HTTP client used to reach the API. The API certificate is always
// verified, against caFile when set and the system roots otherwise; there is no
// skip-verify option. The enrollment token travels in the request body, so only https URLs
// are accepted (see EnrollURL).
func NewAPIClient(caFile string) (*http.Client, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read API CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("API CA file %q contains no valid certificates", caFile)
		}
		cfg.RootCAs = pool
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	return &http.Client{Transport: tr, Timeout: enrollTimeout}, nil
}

// EnrollURL validates the API base URL and returns the enrollment endpoint. A token must not
// cross the network in the clear, so http is refused.
func EnrollURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("OPENVMS_API_URL %q is not a valid URL", base)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("OPENVMS_API_URL must use https (the enrollment token is a secret), got %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + enrollPath
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// Enroll generates a key, sends the CSR with the one-time token to the API and returns the
// verified credentials. The key stays in memory here; the caller persists it. The token is
// never logged or included in errors.
func Enroll(ctx context.Context, hc *http.Client, enrollURL, token string) (*Credentials, error) {
	keyPEM, csrPEM, err := newKeyAndCSR()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"token": token, "csr_pem": string(csrPEM)})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, enrollURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the enrollment endpoint: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, maxEnrollReply))
	if err != nil {
		return nil, fmt.Errorf("read the enrollment reply: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrTokenRejected
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("enrollment failed: HTTP %d: %s", resp.StatusCode, echo(reply))
	}
	var out struct {
		CertificatePEM string `json:"certificate_pem"`
		CAPEM          string `json:"ca_pem"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return nil, fmt.Errorf("decode the enrollment reply: %w", err)
	}
	creds, err := NewCredentials(keyPEM, []byte(out.CertificatePEM), []byte(out.CAPEM))
	if err != nil {
		return nil, fmt.Errorf("the API returned an unusable certificate: %w", err)
	}
	return creds, nil
}

func echo(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrorBodyEcho {
		s = s[:maxErrorBodyEcho] + "..."
	}
	return s
}

// redactURLError drops the URL from a transport error so it carries only the cause.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Ensure returns the agent's credentials. Stored credentials always win and are never
// replaced by an enrollment, so the single-use token is never redeemed twice. The one
// exception is a stored certificate that has already expired together with a token that is
// not the one it was enrolled with: the operator issued a fresh token to recover the agent.
// Without stored credentials a token is required.
func Ensure(ctx context.Context, store Store, token string, enroll func(ctx context.Context, token string) (*Credentials, error), now func() time.Time) (*Credentials, error) {
	stored, err := store.Load()
	if err != nil && !errors.Is(err, ErrNoCredentials) {
		return nil, err
	}
	if stored != nil && !(stored.Expired(now()) && token != "" && !store.TokenUsed(token)) {
		return stored, nil
	}
	if token == "" {
		return nil, errors.New("no stored mTLS credentials and no enrollment token: create a token for this server in the OpenVMS API and set OPENVMS_ENROLL_TOKEN (or OPENVMS_ENROLL_TOKEN_FILE) with OPENVMS_API_URL")
	}
	creds, err := enroll(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := store.Save(creds); err != nil {
		return nil, fmt.Errorf("persist the issued credentials (the enrollment token is now spent): %w", err)
	}
	if err := store.RecordToken(token); err != nil {
		return nil, fmt.Errorf("record the spent enrollment token: %w", err)
	}
	return creds, nil
}
