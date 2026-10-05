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
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	maxOnvifProbeRequestBytes  = 4096
	maxOnvifProbeResponseBytes = 64 << 10
	maxOnvifProbeEndpointBytes = 2048
	maxOnvifProbeUsernameBytes = 256
	maxOnvifProbePasswordBytes = 1024
	maxOnvifProbeServices      = 32
	maxOnvifProbeEndpoints     = 16
	maxOnvifProbeTextRunes     = 256
	onvifProbeTimeout          = 12 * time.Second
)

var (
	ErrAgentProbeUnavailable = errors.New("registered agent probe unavailable")
	ErrAgentProbeFailed      = errors.New("registered agent probe failed")
)

type OnvifProbeRequest struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type OnvifProbeResult struct {
	Information OnvifProbeDeviceInformation `json:"device_information"`
	Services    []OnvifProbeService         `json:"services"`
	Clock       OnvifProbeClock             `json:"system_time"`
}

type OnvifProbeDeviceInformation struct {
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version"`
	SerialNumber    string `json:"serial_number"`
	HardwareID      string `json:"hardware_id"`
}

type OnvifProbeService struct {
	Namespace string            `json:"namespace"`
	XAddrs    []string          `json:"xaddrs"`
	Version   OnvifProbeVersion `json:"version"`
}

type OnvifProbeVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

type OnvifProbeClock struct {
	DateTimeType string             `json:"date_time_type"`
	UTC          OnvifProbeDateTime `json:"utc"`
	Local        OnvifProbeDateTime `json:"local"`
}
type OnvifProbeDateTime struct {
	Time OnvifProbeTime `json:"time"`
	Date OnvifProbeDate `json:"date"`
}
type OnvifProbeTime struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
	Second int `json:"second"`
}
type OnvifProbeDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (s *Service) ProbeOnvif(ctx context.Context, actor authz.Actor, serverID uuid.UUID, request OnvifProbeRequest) (OnvifProbeResult, error) {
	if err := validateOnvifProbeRequest(request); err != nil {
		return OnvifProbeResult{}, err
	}
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return OnvifProbeResult{}, err
		}
		if err := s.requireProbeConfigSecrets(ctx, actor, serverID); err != nil {
			return OnvifProbeResult{}, err
		}
	} else {
		if s.Inv == nil {
			return OnvifProbeResult{}, errors.New("server probe authorization unavailable")
		}
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return OnvifProbeResult{}, err
		}
	}

	var host string
	var port int32
	var token string
	var config AgentTLSConfig
	var err error
	if s.loadProbeAgent != nil {
		host, port, token, config, err = s.loadProbeAgent(ctx, actor, serverID)
	} else {
		host, port, token, config, err = s.loadProbeTLSAndAgent(ctx, actor, serverID)
	}
	if err != nil {
		return OnvifProbeResult{}, err
	}
	if token == "" || net.ParseIP(host) == nil || net.ParseIP(host).To4() == nil || port < 1 || port > 65535 {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	if err := validateAgentTLSConfig(host, config); err != nil {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	useSystemRoots := config.TrustMode == AgentTLSTrustSystem
	client, err := newVerifiedAgentHTTPClient(host, uint16(config.SecurePort), config.CAPEM, useSystemRoots, onvifProbeTimeout, nil, s.probeRoundTripper)
	if err != nil {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	return probeAgentWithClient(ctx, client, token, request)
}

func validateOnvifProbeRequest(in OnvifProbeRequest) error {
	if len(in.Endpoint) == 0 || len(in.Endpoint) > maxOnvifProbeEndpointBytes || len(in.Username) > maxOnvifProbeUsernameBytes || len(in.Password) > maxOnvifProbePasswordBytes ||
		((in.Username == "") != (in.Password == "")) {
		return &ValidationError{Msg: "invalid ONVIF probe request"}
	}
	ep, err := onvif.ParseEndpoint(in.Endpoint)
	if err != nil {
		return &ValidationError{Msg: "invalid ONVIF probe endpoint"}
	}
	u, err := url.Parse(ep.String())
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(in.Endpoint, "\\\r\n\t") {
		return &ValidationError{Msg: "invalid ONVIF probe endpoint"}
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || !addr.Is4() || !validEndpointPort(u.Port()) {
		return &ValidationError{Msg: "ONVIF probe endpoint must use an IPv4 literal"}
	}
	return nil
}

func probeAgent(ctx context.Context, transport http.RoundTripper, host string, port int32, caPEM []byte, useSystemRoots bool, token string, request OnvifProbeRequest) (OnvifProbeResult, error) {
	client, err := newVerifiedAgentHTTPClient(host, uint16(port), caPEM, useSystemRoots, onvifProbeTimeout, nil, transport)
	if err != nil {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	return probeAgentWithClient(ctx, client, token, request)
}

func probeAgentWithClient(ctx context.Context, client *VerifiedAgentHTTPClient, token string, input OnvifProbeRequest) (OnvifProbeResult, error) {
	body, err := json.Marshal(input)
	if err != nil || len(body) > maxOnvifProbeRequestBytes || token == "" {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
	defer cancel()
	response, err := client.Do(ctx, http.MethodPost, "/v1/onvif/probe", http.Header{"Authorization": []string{"Bearer " + token}, "Content-Type": []string{"application/json"}}, bytes.NewReader(body))
	if err != nil {
		return OnvifProbeResult{}, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifProbeResponseBytes+1))
	if err != nil || len(data) > maxOnvifProbeResponseBytes {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	if !validOnvifProbeJSONShape(data) {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var result OnvifProbeResult
	if dec.Decode(&result) != nil {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || !validOnvifProbeResult(result) {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	return sanitizeOnvifProbeResult(result, token, input.Username, input.Password), nil
}

func validOnvifProbeJSONShape(data []byte) bool {
	root, ok := requiredJSONObject(data, "device_information", "services", "system_time")
	if !ok {
		return false
	}
	if _, ok := requiredJSONObject(root["device_information"], "manufacturer", "model", "firmware_version", "serial_number", "hardware_id"); !ok {
		return false
	}
	services, ok := requiredJSONArray(root["services"])
	if !ok || len(services) > maxOnvifProbeServices {
		return false
	}
	for _, rawService := range services {
		service, ok := requiredJSONObject(rawService, "namespace", "xaddrs", "version")
		if !ok {
			return false
		}
		if _, ok := requiredJSONStringArray(service["xaddrs"]); !ok {
			return false
		}
		if _, ok := requiredJSONObject(service["version"], "major", "minor"); !ok {
			return false
		}
	}
	clock, ok := requiredJSONObject(root["system_time"], "date_time_type", "utc", "local")
	if !ok || !validOnvifProbeJSONDateTime(clock["utc"]) || !validOnvifProbeJSONDateTime(clock["local"]) {
		return false
	}
	return true
}

func validOnvifProbeJSONDateTime(raw json.RawMessage) bool {
	dateTime, ok := requiredJSONObject(raw, "time", "date")
	if !ok {
		return false
	}
	if _, ok := requiredJSONObject(dateTime["time"], "hour", "minute", "second"); !ok {
		return false
	}
	if _, ok := requiredJSONObject(dateTime["date"], "year", "month", "day"); !ok {
		return false
	}
	return true
}

func requiredJSONObject(data json.RawMessage, keys ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, false
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return fields, true
}

func requiredJSONArray(data json.RawMessage) ([]json.RawMessage, bool) {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil || items == nil {
		return nil, false
	}
	return items, true
}

func requiredJSONStringArray(data json.RawMessage) ([]string, bool) {
	items, ok := requiredJSONArray(data)
	if !ok {
		return nil, false
	}
	values := make([]string, len(items))
	for i, item := range items {
		if bytes.Equal(bytes.TrimSpace(item), []byte("null")) || json.Unmarshal(item, &values[i]) != nil {
			return nil, false
		}
	}
	return values, true
}

func validOnvifProbeResult(result OnvifProbeResult) bool {
	if result.Services == nil || len(result.Services) > maxOnvifProbeServices {
		return false
	}
	text := []string{result.Information.Manufacturer, result.Information.Model, result.Information.FirmwareVersion, result.Information.SerialNumber, result.Information.HardwareID, result.Clock.DateTimeType}
	for _, value := range text {
		if len([]rune(value)) > maxOnvifProbeTextRunes {
			return false
		}
	}
	for _, svc := range result.Services {
		if len([]rune(svc.Namespace)) > maxOnvifProbeTextRunes || len(svc.XAddrs) > maxOnvifProbeEndpoints || svc.Version.Major < 0 || svc.Version.Major > 10000 || svc.Version.Minor < 0 || svc.Version.Minor > 10000 {
			return false
		}
		for _, address := range svc.XAddrs {
			u, err := url.Parse(address)
			if err != nil || u == nil || u.Hostname() == "" {
				return false
			}
			ip, ipErr := netip.ParseAddr(u.Hostname())
			if ipErr != nil || !ip.Is4() || !validEndpointPort(u.Port()) || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
				return false
			}
		}
	}
	return validClock(result.Clock)
}

func validEndpointPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func validClock(clock OnvifProbeClock) bool {
	for _, value := range []OnvifProbeDateTime{clock.UTC, clock.Local} {
		if value.Time.Hour < 0 || value.Time.Hour > 23 || value.Time.Minute < 0 || value.Time.Minute > 59 || value.Time.Second < 0 || value.Time.Second > 60 || value.Date.Year < 0 || value.Date.Year > 9999 || value.Date.Month < 0 || value.Date.Month > 12 || value.Date.Day < 0 || value.Date.Day > 31 {
			return false
		}
	}
	return true
}

func sanitizeOnvifProbeResult(result OnvifProbeResult, secrets ...string) OnvifProbeResult {
	safe := func(value string) string {
		for _, secret := range secrets {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[redacted]")
			}
		}
		var out strings.Builder
		for _, r := range value {
			if !unicode.IsControl(r) {
				out.WriteRune(r)
			}
		}
		return string([]rune(out.String())[:min(maxOnvifProbeTextRunes, len([]rune(out.String())))])
	}
	result.Information.Manufacturer = safe(result.Information.Manufacturer)
	result.Information.Model = safe(result.Information.Model)
	result.Information.FirmwareVersion = safe(result.Information.FirmwareVersion)
	result.Information.SerialNumber = safe(result.Information.SerialNumber)
	result.Information.HardwareID = safe(result.Information.HardwareID)
	result.Clock.DateTimeType = safe(result.Clock.DateTimeType)
	for i := range result.Services {
		result.Services[i].Namespace = safe(result.Services[i].Namespace)
		for j, addr := range result.Services[i].XAddrs {
			u, _ := url.Parse(addr)
			result.Services[i].XAddrs[j] = u.Scheme + "://" + u.Host
		}
	}
	return result
}

func (s *Service) loadProbeTLSAndAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
	var row db.ServerAgent
	var tlsRow db.ServerAgentTl
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgent(ctx, serverID)
		if err != nil {
			return store.Classify(err)
		}
		tlsRow, err = q.GetServerAgentTLS(ctx, serverID)
		return store.Classify(err)
	})
	if errors.Is(err, store.ErrNotFound) {
		if row.ServerID == uuid.Nil {
			return "", 0, "", AgentTLSConfig{}, ErrAgentUnprovisioned
		}
		return "", 0, "", AgentTLSConfig{}, ErrAgentTLSNotConfigured
	}
	if err != nil {
		return "", 0, "", AgentTLSConfig{}, err
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return "", 0, "", AgentTLSConfig{}, ErrAgentProbeFailed
	}
	return row.Host, row.Port, string(token), configFromRow(tlsRow), nil
}

func (s *Service) requireProbeConfigSecrets(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireServerConfigSecrets != nil {
		return s.requireServerConfigSecrets(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return fmt.Errorf("server config secret authorization unavailable")
	}
	return s.Inv.RequireServerConfigSecrets(ctx, actor, serverID)
}
