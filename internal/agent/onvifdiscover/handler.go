// Package onvifdiscover exposes credential-free, policy-bounded ONVIF discovery.
package onvifdiscover

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

const maxRequestBytes = 1024

type Discoverer interface {
	Probe(context.Context, string) ([]onvif.DiscoveredDevice, error)
}

type Config struct {
	Interfaces   []string
	AllowedCIDRs []string
}

type Handler struct {
	token      string
	discoverer Discoverer
	interfaces map[string]struct{}
	networks   []netip.Prefix
}

type requestBody struct {
	InterfaceName string `json:"interface_name"`
}
type response struct {
	Devices []device `json:"devices"`
}
type device struct {
	XAddrs []string `json:"xaddrs"`
}

func NewHandler(token string, discoverer Discoverer, cfg Config) (*Handler, error) {
	if strings.TrimSpace(token) == "" || discoverer == nil || len(cfg.Interfaces) == 0 || len(cfg.AllowedCIDRs) == 0 {
		return nil, errors.New("ONVIF discovery requires a token, discoverer, interfaces, and allowed CIDRs")
	}
	h := &Handler{token: token, discoverer: discoverer, interfaces: make(map[string]struct{}), networks: make([]netip.Prefix, 0, len(cfg.AllowedCIDRs))}
	for _, name := range cfg.Interfaces {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, ",/\\\r\n\t") {
			return nil, errors.New("invalid ONVIF discovery interface")
		}
		if _, exists := h.interfaces[name]; exists {
			return nil, errors.New("duplicate ONVIF discovery interface")
		}
		h.interfaces[name] = struct{}{}
	}
	for _, raw := range cfg.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() {
			return nil, errors.New("invalid ONVIF allowed CIDR")
		}
		h.networks = append(h.networks, prefix.Masked())
	}
	return h, nil
}

// LoadConfig treats both empty settings as feature-disabled. Partial or malformed
// configuration is an error so an operator cannot accidentally broaden policy.
func LoadConfig(interfaces, cidrs string) (Config, bool, error) {
	interfaces, cidrs = strings.TrimSpace(interfaces), strings.TrimSpace(cidrs)
	if interfaces == "" && cidrs == "" {
		return Config{}, false, nil
	}
	if interfaces == "" || cidrs == "" {
		return Config{}, false, errors.New("both ONVIF_DISCOVERY_INTERFACES and ONVIF_ALLOWED_CIDRS are required")
	}
	return Config{Interfaces: strings.Split(interfaces, ","), AllowedCIDRs: strings.Split(cidrs, ",")}, true, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r, h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input requestBody
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.InterfaceName) == "" || input.InterfaceName != strings.TrimSpace(input.InterfaceName) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, ok := h.interfaces[input.InterfaceName]; !ok {
		http.Error(w, "interface not allowed", http.StatusForbidden)
		return
	}
	devices, err := h.discoverer.Probe(r.Context(), input.InterfaceName)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "request canceled", http.StatusRequestTimeout)
			return
		}
		http.Error(w, `{"error":"discovery_failed"}`, http.StatusBadGateway)
		return
	}
	result := response{Devices: make([]device, 0, len(devices))}
	for _, found := range devices {
		item := device{XAddrs: make([]string, 0, len(found.XAddrs))}
		for _, endpoint := range found.XAddrs {
			u, err := neturlParse(endpoint.String())
			if err != nil {
				continue
			}
			addr, err := netip.ParseAddr(u.Hostname())
			if err != nil || !addr.Is4() {
				continue
			}
			for _, prefix := range h.networks {
				if prefix.Contains(addr) {
					item.XAddrs = append(item.XAddrs, endpoint.String())
					break
				}
			}
		}
		if len(item.XAddrs) > 0 {
			result.Devices = append(result.Devices, item)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func authorized(r *http.Request, token string) bool {
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	return strings.HasPrefix(got, prefix) && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(got, prefix)), []byte(token)) == 1
}

// neturlParse stays small and rejects malformed/non-HTTP endpoints defensively.
func neturlParse(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
		return nil, errors.New("invalid endpoint")
	}
	return u, nil
}
