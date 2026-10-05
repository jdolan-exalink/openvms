// Package onvif contains the bounded SOAP transport used by ONVIF clients.
package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const soapNamespace = "http://www.w3.org/2003/05/soap-envelope"

var (
	ErrResponseTooLarge = errors.New("ONVIF response exceeds configured limit")
	ErrInvalidEndpoint  = errors.New("invalid ONVIF endpoint")
)

// Endpoint is a validated, credential-free HTTP(S) service URL.
type Endpoint struct{ value string }

func ParseEndpoint(raw string) (Endpoint, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\t") {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return Endpoint{value: u.String()}, nil
}

type Operation uint8

const (
	ReadOnly Operation = iota
	Mutating
)

type Status string

const (
	StatusSupported   Status = "supported"
	StatusUnsupported Status = "unsupported"
	StatusDegraded    Status = "degraded"
	StatusFailed      Status = "failed"
)

// CallError exposes stable, redacted failure details while retaining the cause for errors.Is.
type CallError struct {
	Status Status
	Code   string
	cause  error
}

func (e *CallError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ONVIF call %s: %s", e.Status, e.Code)
	}
	return fmt.Sprintf("ONVIF call %s", e.Status)
}
func (e *CallError) Unwrap() error { return e.cause }

type Config struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	MaxAttempts  int
	RetryDelay   time.Duration
}

type Client struct {
	transport http.RoundTripper
	config    Config
}

func NewClient(transport http.RoundTripper, config Config) *Client {
	if transport == nil {
		transport = http.DefaultTransport
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 1 << 20
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.MaxAttempts > 3 {
		config.MaxAttempts = 3
	}
	if config.RetryDelay < 0 {
		config.RetryDelay = 0
	}
	return &Client{transport: transport, config: config}
}

type soapEnvelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    soapBody `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}
type soapBody struct {
	Inner string `xml:",innerxml"`
}
type soapFault struct {
	Code struct {
		Value   string `xml:"Value"`
		Subcode struct {
			Value string `xml:"Value"`
		} `xml:"Subcode"`
	} `xml:"Code"`
	Reason struct {
		Text string `xml:"Text"`
	} `xml:"Reason"`
}

// Call performs one SOAP operation. Only explicitly read-only calls may retry transport or 5xx failures.
func (c *Client) Call(ctx context.Context, endpoint Endpoint, action, operationXML string, operation Operation) ([]byte, error) {
	if endpoint.value == "" {
		return nil, ErrInvalidEndpoint
	}
	if action == "" || strings.ContainsAny(action, "\r\n\"") {
		return nil, &CallError{Status: StatusFailed, Code: "invalid_action"}
	}
	if _, err := ParseEndpoint(endpoint.value); err != nil {
		return nil, err
	}
	body := []byte(`<s:Envelope xmlns:s="` + soapNamespace + `"><s:Body>` + operationXML + `</s:Body></s:Envelope>`)
	var requestEnvelope soapEnvelope
	if err := xml.Unmarshal(body, &requestEnvelope); err != nil {
		return nil, &CallError{Status: StatusFailed, Code: "invalid_request_xml"}
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	attempts := 1
	if operation == ReadOnly {
		attempts = c.config.MaxAttempts
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.value, bytes.NewReader(body))
		if err != nil {
			return nil, &CallError{Status: StatusFailed, Code: "request_failed", cause: err}
		}
		req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)
		resp, err := c.transport.RoundTrip(req)
		if err != nil {
			last = err
			if i+1 < attempts && wait(ctx, c.config.RetryDelay) {
				continue
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &CallError{Status: StatusDegraded, Code: "transport_error", cause: err}
		}
		data, readErr := readBounded(resp.Body, c.config.MaxBodyBytes)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, &CallError{Status: StatusFailed, Code: "authentication_failed"}
		}
		var envelope soapEnvelope
		soapErr := xml.Unmarshal(data, &envelope)
		if soapErr != nil || strings.TrimSpace(envelope.Body.Inner) == "" {
			if resp.StatusCode >= 500 && i+1 < attempts && wait(ctx, c.config.RetryDelay) {
				continue
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				status := StatusFailed
				if resp.StatusCode >= 500 {
					status = StatusDegraded
				}
				return nil, &CallError{Status: status, Code: "http_error"}
			}
			return nil, &CallError{Status: StatusFailed, Code: "invalid_soap_response"}
		}
		var fault soapFault
		if err := xml.Unmarshal([]byte(envelope.Body.Inner), &fault); err == nil && (fault.Code.Value != "" || fault.Code.Subcode.Value != "") {
			code := fault.Code.Subcode.Value
			if code == "" {
				code = fault.Code.Value
			}
			status := StatusFailed
			if strings.Contains(strings.ToLower(code), "unsupported") || strings.Contains(strings.ToLower(code), "actionnotsupported") {
				status = StatusUnsupported
			} else if strings.Contains(strings.ToLower(fault.Code.Value), "receiver") {
				status = StatusDegraded
				if operation == ReadOnly && resp.StatusCode >= 500 && i+1 < attempts && wait(ctx, c.config.RetryDelay) {
					continue
				}
			}
			return nil, &CallError{Status: status, Code: "soap_fault"}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode >= 500 && operation == ReadOnly && i+1 < attempts && wait(ctx, c.config.RetryDelay) {
				continue
			}
			status := StatusFailed
			if resp.StatusCode >= 500 {
				status = StatusDegraded
			}
			return nil, &CallError{Status: status, Code: "http_error"}
		}
		// Return the complete SOAP document. Namespace declarations on Envelope
		// or Body are in scope for operation elements and must remain available
		// to encoding/xml decoders.
		return bytes.Clone(data), nil
	}
	return nil, &CallError{Status: StatusDegraded, Code: "transport_error", cause: last}
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, &CallError{Status: StatusFailed, Code: "response_read_failed", cause: err}
	}
	if int64(len(b)) > max {
		return nil, ErrResponseTooLarge
	}
	return b, nil
}

func wait(ctx context.Context, delay time.Duration) bool {
	if delay == 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
