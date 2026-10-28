// Package go2rtc is the OpenVMS side of the go2rtc container that plays the cameras of Provision-ISR
// recorders. It registers a stream (whose source carries the recorder's credentials) and says where its
// MSE websocket is. The container is reachable only on a private network; this client never puts a
// stream source in an error, a log line or a returned value.
package go2rtc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrUnavailable reports that go2rtc did not answer or answered with an error.
var ErrUnavailable = errors.New("go2rtc is not available")

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// Client talks to one go2rtc instance.
type Client struct {
	base *url.URL
	http *http.Client
}

// New returns a client for the go2rtc API at baseURL ("http://go2rtc:1984").
func New(baseURL string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("go2rtc: the URL must look like http://host:port")
	}
	return &Client{base: u, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

// ValidName reports whether name can be used as a stream name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Put registers a stream under name, or replaces its source. src is a go2rtc source, for example
// "ffmpeg:rtsp://user:pass@host/path#video=copy".
func (c *Client) Put(ctx context.Context, name, src string) error {
	if !ValidName(name) {
		return fmt.Errorf("go2rtc: invalid stream name")
	}
	q := url.Values{"name": {name}, "src": {src}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base.String()+"/api/streams?"+q.Encode(), nil)
	if err != nil {
		return ErrUnavailable
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// net/http repeats the request URL, which here carries the source: drop it.
		return fmt.Errorf("%w: %s", ErrUnavailable, "the request failed")
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: it answered %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}

// Ping checks that the API answers.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/api", nil)
	if err != nil {
		return ErrUnavailable
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnavailable, "the request failed")
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: it answered %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}

// WebSocketURL is where the MSE websocket of a registered stream is.
func (c *Client) WebSocketURL(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("go2rtc: invalid stream name")
	}
	scheme := "ws"
	if c.base.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + c.base.Host + "/api/ws?" + url.Values{"src": {name}}.Encode(), nil
}
