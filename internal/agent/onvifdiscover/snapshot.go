package onvifdiscover

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"image/jpeg"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxOnvifSnapshotBytes = 8 << 20
const maxOnvifSnapshotPixels = 32 << 20

func fetchOnvifSnapshot(ctx context.Context, endpointHost, rawURI, username, password string, transport http.RoundTripper) ([]byte, error) {
	u, err := url.Parse(rawURI)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(rawURI, "\\\r\n\t") {
		return nil, errors.New("invalid snapshot URI")
	}
	selected, err := netip.ParseAddr(endpointHost)
	if err != nil || !selected.Is4() {
		return nil, errors.New("invalid selected camera host")
	}
	target, err := netip.ParseAddr(u.Hostname())
	if err != nil || !target.Is4() || target != selected {
		return nil, errors.New("snapshot URI host does not match selected camera")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid snapshot URI port")
		}
	}
	if username != "" && u.Scheme != "https" {
		return nil, errors.New("camera credentials require HTTPS")
	}
	if transport == nil {
		transport = &http.Transport{
			Proxy:           nil,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:     (&net.Dialer{Timeout: probeOperationTimeout, KeepAlive: 30 * time.Second}).DialContext,
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   probeOperationTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid snapshot request")
	}
	req.Header.Set("Accept", "image/jpeg")
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("snapshot request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("snapshot returned non-success status")
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "image/jpeg") {
		return nil, errors.New("snapshot is not JPEG")
	}
	if resp.ContentLength > maxOnvifSnapshotBytes {
		return nil, errors.New("snapshot is too large")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOnvifSnapshotBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxOnvifSnapshotBytes {
		return nil, errors.New("snapshot body is invalid or too large")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > maxOnvifSnapshotPixels {
		return nil, errors.New("snapshot is malformed JPEG")
	}
	return body, nil
}
