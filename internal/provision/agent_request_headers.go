package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	maxAgentRequestHeaders     = 8
	maxAgentRequestHeaderName  = 64
	maxAgentRequestHeaderValue = 1024
)

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// reservedAgentHeaders are set by OpenVMS or by the HTTP stack; a stored header can never replace them.
var reservedAgentHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "host": true, "content-length": true,
	"transfer-encoding": true, "connection": true, "upgrade": true, "te": true, "trailer": true,
}

// validateAgentRequestHeaders checks the extra headers an operator stores for an agent: the service
// token a Zero Trust gateway in front of it asks for (for example CF-Access-Client-Id and
// CF-Access-Client-Secret).
func validateAgentRequestHeaders(headers map[string]string) error {
	if len(headers) > maxAgentRequestHeaders {
		return fmt.Errorf("at most %d request headers", maxAgentRequestHeaders)
	}
	for name, value := range headers {
		if len(name) == 0 || len(name) > maxAgentRequestHeaderName || !headerNamePattern.MatchString(name) {
			return errors.New("invalid request header name")
		}
		if reservedAgentHeaders[strings.ToLower(name)] {
			return fmt.Errorf("the %s header is set by OpenVMS and cannot be stored", name)
		}
		if len(value) > maxAgentRequestHeaderValue {
			return errors.New("request header value is too long")
		}
		for _, r := range value {
			if r < ' ' && r != '\t' || r == 0x7f {
				return errors.New("request header value has control characters")
			}
		}
	}
	return nil
}

// The additional data keeps a sealed headers blob from being swapped into the token column.
func headersAAD(serverID uuid.UUID) []byte {
	return append(serverID[:len(serverID):len(serverID)], "|request-headers"...)
}

func sealAgentRequestHeaders(sealer *secrets.Sealer, serverID uuid.UUID, headers map[string]string) ([]byte, error) {
	if err := validateAgentRequestHeaders(headers); err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		return nil, nil
	}
	plain, err := json.Marshal(headers)
	if err != nil {
		return nil, err
	}
	return sealer.Seal(plain, headersAAD(serverID))
}

func openAgentRequestHeaders(sealer *secrets.Sealer, serverID uuid.UUID, sealed []byte) (map[string]string, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	plain, err := sealer.Open(sealed, headersAAD(serverID))
	if err != nil {
		return nil, err
	}
	var headers map[string]string
	if err := json.Unmarshal(plain, &headers); err != nil {
		return nil, err
	}
	return headers, validateAgentRequestHeaders(headers)
}

type headerDoer struct {
	inner   AgentProbeDoer
	headers map[string]string
}

// withAgentRequestHeaders adds the stored gateway headers to every request. Headers the caller sets
// (the bearer token above all) win, and the caller's map is not modified.
func withAgentRequestHeaders(inner AgentProbeDoer, headers map[string]string) AgentProbeDoer {
	if len(headers) == 0 {
		return inner
	}
	return headerDoer{inner: inner, headers: headers}
}

func (d headerDoer) Do(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error) {
	merged := headers.Clone()
	if merged == nil {
		merged = make(http.Header)
	}
	for name, value := range d.headers {
		if _, set := merged[http.CanonicalHeaderKey(name)]; !set {
			merged.Set(name, value)
		}
	}
	return d.inner.Do(ctx, method, path, merged, body)
}

// SetAgentRequestHeaders stores, sealed, the extra headers sent with every request to a server's agent
// (an empty map clears them). They are never returned by any API.
func (s *Service) SetAgentRequestHeaders(ctx context.Context, actor authz.Actor, serverID uuid.UUID, headers map[string]string) error {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return err
	}
	sealed, err := sealAgentRequestHeaders(s.Sealer, serverID, headers)
	if err != nil {
		return &ValidationError{Msg: err.Error()}
	}
	var rows int64
	err = s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		rows, err = q.UpdateServerAgentRequestHeaders(ctx, db.UpdateServerAgentRequestHeadersParams{ServerID: serverID, RequestHeadersSealed: sealed})
		return err
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAgentUnprovisioned
	}
	return nil
}
