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
	maxOnvifProbeProfiles      = 32
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

type OnvifProbeProfile struct {
	Token                     string  `json:"token"`
	Name                      string  `json:"name"`
	Fixed                     bool    `json:"fixed"`
	MediaVersion              string  `json:"media_version"`
	SourceConfigurationToken  string  `json:"source_configuration_token"`
	EncoderConfigurationToken string  `json:"encoder_configuration_token"`
	PTZConfigurationToken     string  `json:"ptz_configuration_token"`
	Encoding                  string  `json:"encoding"`
	Width                     int     `json:"width"`
	Height                    int     `json:"height"`
	FPS                       int     `json:"fps"`
	FrameRate                 float64 `json:"frame_rate"`
	Bitrate                   int     `json:"bitrate"`
	StreamURI                 string  `json:"stream_uri"`
}

type OnvifProbeStreams struct {
	MainURL string `json:"main_url"`
	SubURL  string `json:"sub_url"`
}

type OnvifProbeAnalytics struct {
	SupportedTopics []string `json:"supported_topics"`
}

type OnvifProbePTZ struct {
	Supported bool              `json:"supported"`
	Presets   []onvif.PTZPreset `json:"presets,omitempty"`
}

type OnvifProbeResult struct {
	Information OnvifProbeDeviceInformation `json:"device_information"`
	Services    []OnvifProbeService         `json:"services"`
	Clock       OnvifProbeClock             `json:"system_time"`
	Profiles    []OnvifProbeProfile         `json:"profiles,omitempty"`
	Streams     OnvifProbeStreams           `json:"streams,omitempty"`
	Analytics   OnvifProbeAnalytics         `json:"analytics,omitempty"`
	PTZ         OnvifProbePTZ               `json:"ptz,omitempty"`
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
		if request.Username == "" {
			if srvUser, srvPass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && srvUser != "" {
				request.Username = srvUser
				request.Password = srvPass
			}
		}
	}

	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return OnvifProbeResult{}, err
	}
	return probeAgentWithClient(ctx, client, token, request)
}

func (s *Service) getAgentClientAndToken(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentProbeDoer, string, error) {
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
		return nil, "", err
	}
	baseURL, gatewayHeaders, err := s.agentConnection(ctx, actor, serverID)
	if err != nil {
		// Say why without the sealed values: an unreadable blob (a changed key) looks like a network failure otherwise.
		s.logger().Warn("agent connection settings cannot be read", "server_id", serverID, "error", err)
		return nil, "", ErrAgentProbeFailed
	}
	endpoint, err := AgentEndpointFromRow(host, port, baseURL)
	if token == "" || err != nil {
		return nil, "", ErrAgentProbeFailed
	}
	if baseURL != "" {
		client, err := s.endpointClient(endpoint, config)
		if err != nil {
			return nil, "", ErrAgentProbeFailed
		}
		return withAgentRequestHeaders(client, gatewayHeaders), token, nil
	}
	var client AgentProbeDoer
	if config.TrustMode == AgentTLSTrustPlaintext {
		// #nosec G115 -- AgentEndpointFromRow constrains the port to 1..65535.
		plainClient, err := newPlainAgentHTTPClient(endpoint.Host, uint16(endpoint.Port), onvifProbeTimeout, s.probeRoundTripper)
		if err != nil {
			return nil, "", ErrAgentProbeFailed
		}
		client = plainClient
	} else {
		if err := validateAgentTLSConfig(host, config); err != nil {
			return nil, "", ErrAgentProbeFailed
		}
		useSystemRoots := config.TrustMode == AgentTLSTrustSystem
		// #nosec G115 -- validateAgentTLSConfig constrains the port to 1..65535 above.
		httpsClient, err := newVerifiedAgentHTTPClient(endpoint.Host, uint16(config.SecurePort), config.CAPEM, useSystemRoots, onvifProbeTimeout, nil, s.probeRoundTripper)
		if err != nil {
			return nil, "", ErrAgentProbeFailed
		}
		client = httpsClient
	}
	return client, token, nil
}

// endpointClient builds the client of an agent that has a stored base URL. HTTPS verifies the host
// name with the system roots unless the server has its own CA configured; plain HTTP is for a
// private address (LAN or VPN). Either way the destination is checked when the connection is made.
func (s *Service) endpointClient(endpoint AgentEndpoint, config AgentTLSConfig) (AgentProbeDoer, error) {
	// #nosec G115 -- ParseAgentEndpoint constrains the port to 1..65535.
	port := uint16(endpoint.Port)
	if endpoint.Scheme == "http" {
		client, err := newPlainAgentHTTPClient(endpoint.Host, port, onvifProbeTimeout, s.probeRoundTripper)
		if err == nil && s.probeRoundTripper == nil {
			err = guardAgentTransport(client.client, "http")
		}
		return client, err
	}
	var client *VerifiedAgentHTTPClient
	var err error
	if config.TrustMode == AgentTLSTrustCustom && len(config.CAPEM) != 0 {
		client, err = newVerifiedAgentHTTPClient(endpoint.Host, port, config.CAPEM, false, onvifProbeTimeout, nil, s.probeRoundTripper)
	} else {
		client, err = newVerifiedAgentHTTPClient(endpoint.Host, port, nil, true, onvifProbeTimeout, nil, s.probeRoundTripper)
	}
	if err == nil && s.probeRoundTripper == nil {
		err = guardAgentTransport(client.client, "https")
	}
	return client, err
}

// guardAgentTransport makes the client connect only to approved destinations (see
// agent_dial_guard.go). A transport it cannot control is an error: the client is never left unguarded.
func guardAgentTransport(client *http.Client, scheme string) error {
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return errors.New("agent client transport cannot be guarded")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport.DialContext = guardedDialContext(scheme, systemLookup, dialer.DialContext)
	return nil
}

// agentConnection returns the stored base URL of the server's agent (empty when it uses host and
// port) and the gateway headers sealed with it.
func (s *Service) agentConnection(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (string, map[string]string, error) {
	if s.loadAgentConnection != nil {
		return s.loadAgentConnection(ctx, actor, serverID)
	}
	if s.Store == nil {
		return "", nil, nil
	}
	row, err := s.agentRow(ctx, actor, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if row.BaseUrl == "" {
		return "", nil, nil
	}
	headers, err := openAgentRequestHeaders(s.Sealer, serverID, row.RequestHeadersSealed)
	if err != nil {
		return "", nil, err
	}
	return row.BaseUrl, headers, nil
}

func validateOnvifProbeRequest(in OnvifProbeRequest) error {
	if len(in.Endpoint) == 0 || len(in.Endpoint) > maxOnvifProbeEndpointBytes || len(in.Username) > maxOnvifProbeUsernameBytes || len(in.Password) > maxOnvifProbePasswordBytes ||
		(in.Username == "" && in.Password != "") {
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

// AgentProbeDoer performs HTTP requests against an edge-agent endpoint.
type AgentProbeDoer interface {
	Do(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error)
}

// PlainAgentHTTPClient communicates with an unencrypted edge-agent over plain HTTP.
type PlainAgentHTTPClient struct {
	baseURL string
	client  *http.Client
}

func newPlainAgentHTTPClient(registeredHost string, port uint16, timeout time.Duration, testTransport http.RoundTripper) (*PlainAgentHTTPClient, error) {
	host := strings.ToLower(registeredHost)
	if !validAgentHost(host) || port == 0 || timeout <= 0 {
		return nil, errors.New("invalid registered agent HTTP configuration")
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ResponseHeaderTimeout: timeout,
	}
	var roundTripper http.RoundTripper = transport
	if testTransport != nil {
		roundTripper = testTransport
	}
	client := &http.Client{
		Timeout:       timeout,
		Transport:     roundTripper,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &PlainAgentHTTPClient{
		baseURL: "http://" + net.JoinHostPort(host, strconv.Itoa(int(port))),
		client:  client,
	}, nil
}

func (c *PlainAgentHTTPClient) Do(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("plain agent client is unavailable")
	}
	parsed, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(path, "\\\r\n\t") {
		return nil, errors.New("agent request path must be relative and query-free")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, errors.New("agent request is invalid")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	return c.client.Do(req)
}

func probeAgent(ctx context.Context, transport http.RoundTripper, host string, port int32, caPEM []byte, useSystemRoots bool, token string, request OnvifProbeRequest) (OnvifProbeResult, error) {
	if port < 1 || port > 65535 {
		return OnvifProbeResult{}, ErrAgentProbeFailed
	}
	var client AgentProbeDoer
	if caPEM == nil && !useSystemRoots {
		var err error
		client, err = newPlainAgentHTTPClient(host, uint16(port), onvifProbeTimeout, transport)
		if err != nil {
			return OnvifProbeResult{}, ErrAgentProbeFailed
		}
	} else {
		// #nosec G115 -- the port is range-checked immediately above.
		var err error
		client, err = newVerifiedAgentHTTPClient(host, uint16(port), caPEM, useSystemRoots, onvifProbeTimeout, nil, transport)
		if err != nil {
			return OnvifProbeResult{}, ErrAgentProbeFailed
		}
	}
	return probeAgentWithClient(ctx, client, token, request)
}

func probeAgentWithClient(ctx context.Context, client AgentProbeDoer, token string, input OnvifProbeRequest) (OnvifProbeResult, error) {
	// #nosec G117 -- the credential is sent only to the verified agent over HTTPS and is never logged.
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
	if rawProfiles, exists := root["profiles"]; exists {
		profiles, ok := requiredJSONArray(rawProfiles)
		if !ok || len(profiles) > maxOnvifProbeProfiles {
			return false
		}
		for _, rawProfile := range profiles {
			profile, ok := requiredJSONObject(rawProfile, "token", "name", "fixed", "media_version", "source_configuration_token", "encoder_configuration_token", "ptz_configuration_token", "encoding", "width", "height", "fps", "frame_rate", "bitrate", "stream_uri")
			if !ok {
				return false
			}
			for _, key := range []string{"token", "name", "media_version", "source_configuration_token", "encoder_configuration_token", "ptz_configuration_token", "encoding", "stream_uri"} {
				var value string
				if json.Unmarshal(profile[key], &value) != nil {
					return false
				}
			}
			var fixed bool
			if json.Unmarshal(profile["fixed"], &fixed) != nil {
				return false
			}
		}
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
	if result.Services == nil || len(result.Services) > maxOnvifProbeServices || len(result.Profiles) > maxOnvifProbeProfiles {
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
	for _, profile := range result.Profiles {
		for _, value := range []string{profile.Token, profile.Name, profile.MediaVersion, profile.SourceConfigurationToken, profile.EncoderConfigurationToken, profile.PTZConfigurationToken, profile.Encoding} {
			if len([]rune(value)) > maxOnvifProbeTextRunes {
				return false
			}
		}
		if profile.Width < 0 || profile.Width > 100000 || profile.Height < 0 || profile.Height > 100000 || profile.FPS < 0 || profile.FPS > 10000 || profile.FrameRate < 0 || profile.FrameRate > 10000 || profile.Bitrate < 0 || profile.Bitrate > 100000000 {
			return false
		}
		if profile.Token == "" || (profile.MediaVersion != "1.0" && profile.MediaVersion != "2.0") || len(profile.StreamURI) > 2048 {
			return false
		}
		if profile.StreamURI != "" {
			u, err := url.Parse(profile.StreamURI)
			if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" {
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
	for i := range result.Profiles {
		result.Profiles[i].Token = safe(result.Profiles[i].Token)
		result.Profiles[i].Name = safe(result.Profiles[i].Name)
		result.Profiles[i].MediaVersion = safe(result.Profiles[i].MediaVersion)
		result.Profiles[i].SourceConfigurationToken = safe(result.Profiles[i].SourceConfigurationToken)
		result.Profiles[i].EncoderConfigurationToken = safe(result.Profiles[i].EncoderConfigurationToken)
		result.Profiles[i].PTZConfigurationToken = safe(result.Profiles[i].PTZConfigurationToken)
		result.Profiles[i].Encoding = safe(result.Profiles[i].Encoding)
		result.Profiles[i].StreamURI = sanitizeOnvifStreamURI(result.Profiles[i].StreamURI, secrets...)
	}
	result.Streams.MainURL = sanitizeOnvifStreamURI(result.Streams.MainURL, secrets...)
	result.Streams.SubURL = sanitizeOnvifStreamURI(result.Streams.SubURL, secrets...)
	return result
}

func sanitizeOnvifStreamURI(raw string, secrets ...string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	safe := u.String()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(safe, secret) {
			return ""
		}
	}
	return safe
}

func (s *Service) loadProbeTLSAndAgent(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (string, int32, string, AgentTLSConfig, error) {
	var row db.ServerAgent
	var tlsRow db.ServerAgentTl
	var tlsErr error
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgent(ctx, serverID)
		if err != nil {
			return store.Classify(err)
		}
		tlsRow, tlsErr = q.GetServerAgentTLS(ctx, serverID)
		tlsErr = store.Classify(tlsErr)
		return nil
	})
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.ServerID == uuid.Nil) {
		return "", 0, "", AgentTLSConfig{}, ErrAgentUnprovisioned
	}
	if err != nil {
		return "", 0, "", AgentTLSConfig{}, err
	}
	token, err := s.Sealer.Open(row.TokenSealed, row.ServerID[:])
	if err != nil {
		return "", 0, "", AgentTLSConfig{}, ErrAgentProbeFailed
	}
	if errors.Is(tlsErr, store.ErrNotFound) {
		var securePort uint32
		if row.Port > 0 {
			// #nosec G115 -- safe positive port conversion
			securePort = uint32(row.Port)
		}
		return row.Host, row.Port, string(token), AgentTLSConfig{SecurePort: securePort, TrustMode: AgentTLSTrustPlaintext}, nil
	}
	if tlsErr != nil {
		return "", 0, "", AgentTLSConfig{}, tlsErr
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
