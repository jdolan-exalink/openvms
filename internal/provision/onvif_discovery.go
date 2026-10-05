package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxDiscoveryResponse = 1 << 20

// DiscoveryDevice is a credential-free subset of the edge-agent discovery response.
type DiscoveryDevice struct {
	XAddrs []string `json:"xaddrs"`
}
type DiscoveryResult struct {
	Devices []DiscoveryDevice `json:"devices"`
}
type discoveryRequest struct {
	InterfaceName string `json:"interface_name"`
}

// Discovery failures are deliberately typed and redacted at the service boundary.
var (
	ErrAgentUnavailable          = errors.New("registered agent unavailable")
	ErrAgentUnprovisioned        = errors.New("server agent is not provisioned")
	ErrAgentDiscoveryUnsupported = errors.New("agent discovery is unsupported or not permitted")
	ErrAgentDiscoveryUnknown     = errors.New("agent discovery failed")
)

// DiscoverAgent relays a narrow credential-free request to the registered edge agent.
func discoverAgent(ctx context.Context, client *http.Client, host string, port int32, token, interfaceName string) (DiscoveryResult, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || port < 1 || port > 65535 || token == "" || strings.TrimSpace(interfaceName) == "" {
		return DiscoveryResult{}, errors.New("invalid registered agent discovery configuration")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	// Copy the injected client but never inherit a redirect policy that could send the
	// registered agent bearer token to another endpoint.
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &clone
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, _ := json.Marshal(discoveryRequest{InterfaceName: strings.TrimSpace(interfaceName)})
	endpointURL := "http://" + net.JoinHostPort(ip.To4().String(), strconv.Itoa(int(port))) + "/v1/onvif/discover"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
	if err != nil {
		return DiscoveryResult{}, errors.New("agent discovery request failed")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return DiscoveryResult{}, ctx.Err()
		}
		return DiscoveryResult{}, ErrAgentUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound, http.StatusForbidden:
			return DiscoveryResult{}, ErrAgentDiscoveryUnsupported
		case http.StatusRequestTimeout:
			if ctx.Err() != nil {
				return DiscoveryResult{}, ctx.Err()
			}
			return DiscoveryResult{}, ErrAgentUnavailable
		case http.StatusBadGateway:
			return DiscoveryResult{}, ErrAgentDiscoveryUnknown
		default:
			return DiscoveryResult{}, ErrAgentUnavailable
		}
	}
	limited := io.LimitReader(resp.Body, maxDiscoveryResponse+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > maxDiscoveryResponse {
		return DiscoveryResult{}, fmt.Errorf("invalid agent discovery response")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out DiscoveryResult
	if err := dec.Decode(&out); err != nil {
		return DiscoveryResult{}, errors.New("invalid agent discovery response")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return DiscoveryResult{}, errors.New("invalid agent discovery response")
	}
	if out.Devices == nil || len(out.Devices) > 256 {
		return DiscoveryResult{}, errors.New("invalid agent discovery response")
	}
	for i := range out.Devices {
		if out.Devices[i].XAddrs == nil {
			return DiscoveryResult{}, errors.New("invalid agent discovery response")
		}

		if len(out.Devices[i].XAddrs) > 16 {
			return DiscoveryResult{}, errors.New("invalid agent discovery response")
		}
		for _, address := range out.Devices[i].XAddrs {
			u, parseErr := url.Parse(address)
			if parseErr != nil || u == nil || len(address) > 2048 {
				return DiscoveryResult{}, errors.New("invalid agent discovery response")
			}
			ip := net.ParseIP(u.Hostname())
			if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || ip == nil || ip.To4() == nil || strings.ContainsAny(address, "\\\r\n\t") {
				return DiscoveryResult{}, errors.New("invalid agent discovery response")
			}
		}
	}
	return out, nil
}
