package onvifdiscover

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/jdolan-exalink/openvms/internal/onvif"
)

const (
	maxProbeRequestBytes  = 4096
	maxProbeResponseBytes = 64 << 10
	maxProbeEndpointBytes = 2048
	maxProbeUsernameBytes = 256
	maxProbePasswordBytes = 1024
	maxProbeFieldRunes    = 256
	maxProbeServices      = 32
	probeOperationTimeout = 4 * time.Second
	probeOverallTimeout   = 10 * time.Second
)

type ProbeProfile struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Encoding  string `json:"encoding"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FPS       int    `json:"fps"`
	Bitrate   int    `json:"bitrate"`
	StreamURI string `json:"stream_uri"`
}

type ProbeStreams struct {
	MainURL string `json:"main_url"`
	SubURL  string `json:"sub_url"`
}

type ProbeAnalytics struct {
	SupportedTopics []string `json:"supported_topics"`
}

type ProbePTZ struct {
	Supported bool              `json:"supported"`
	Presets   []onvif.PTZPreset `json:"presets,omitempty"`
}

type ProbeResult struct {
	Information onvif.DeviceInformation
	Services    []onvif.Service
	Clock       onvif.DeviceClock
	Profiles    []ProbeProfile
	Streams     ProbeStreams
	Analytics   ProbeAnalytics
	PTZ         ProbePTZ
}

// DeviceProber executes a bounded, read-only ONVIF device probe.
type DeviceProber func(context.Context, onvif.Endpoint, string, string) (ProbeResult, error)

type probeRequest struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProbeHandler struct {
	token    string
	probe    DeviceProber
	networks []netip.Prefix
}

type probeResponse struct {
	Information probeDeviceInformation `json:"device_information"`
	Services    []probeService         `json:"services"`
	Clock       probeClock             `json:"system_time"`
	Profiles    []ProbeProfile         `json:"profiles,omitempty"`
	Streams     ProbeStreams           `json:"streams,omitempty"`
	Analytics   ProbeAnalytics         `json:"analytics,omitempty"`
	PTZ         ProbePTZ               `json:"ptz,omitempty"`
}

type probeClock struct {
	DateTimeType string        `json:"date_time_type"`
	UTC          probeDateTime `json:"utc"`
	Local        probeDateTime `json:"local"`
}

type probeDateTime struct {
	Time probeTime `json:"time"`
	Date probeDate `json:"date"`
}

type probeTime struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
	Second int `json:"second"`
}

type probeDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type probeVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

type probeDeviceInformation struct {
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version"`
	SerialNumber    string `json:"serial_number"`
	HardwareID      string `json:"hardware_id"`
}

type probeService struct {
	Namespace string       `json:"namespace"`
	XAddrs    []string     `json:"xaddrs"`
	Version   probeVersion `json:"version"`
}

func NewProbeHandler(token string, cfg Config, prober DeviceProber) (*ProbeHandler, error) {
	if strings.TrimSpace(token) == "" || prober == nil || len(cfg.AllowedCIDRs) == 0 {
		return nil, errors.New("ONVIF probe requires an agent token, prober, and allowed CIDRs")
	}
	h := &ProbeHandler{token: token, probe: prober, networks: make([]netip.Prefix, 0, len(cfg.AllowedCIDRs))}
	for _, raw := range cfg.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() {
			return nil, errors.New("invalid ONVIF allowed CIDR")
		}
		h.networks = append(h.networks, prefix.Masked())
	}
	return h, nil
}

func (h *ProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r, h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProbeRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input probeRequest
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	endpoint, address, err := parseProbeEndpoint(input.Endpoint)
	if err != nil {
		http.Error(w, "invalid endpoint", http.StatusBadRequest)
		return
	}
	if !h.allowed(address) {
		http.Error(w, "endpoint not allowed", http.StatusForbidden)
		return
	}
	if len(input.Endpoint) > maxProbeEndpointBytes || len(input.Username) > maxProbeUsernameBytes || len(input.Password) > maxProbePasswordBytes ||
		((input.Username == "") != (input.Password == "")) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), probeOverallTimeout)
	defer cancel()
	result, err := h.probe(ctx, endpoint, input.Username, input.Password)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "request canceled", http.StatusRequestTimeout)
			return
		}
		http.Error(w, `{"error":"probe_failed"}`, http.StatusBadGateway)
		return
	}
	projected := projectProbeResult(result, h.networks, h.token, input.Username, input.Password)
	body, err := json.Marshal(projected)
	if err != nil || len(body) > maxProbeResponseBytes {
		http.Error(w, `{"error":"probe_failed"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(body, '\n'))
}

func (h *ProbeHandler) allowed(address netip.Addr) bool {
	for _, prefix := range h.networks {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseProbeEndpoint(raw string) (onvif.Endpoint, netip.Addr, error) {
	if raw == "" || len(raw) > maxProbeEndpointBytes {
		return onvif.Endpoint{}, netip.Addr{}, onvif.ErrInvalidEndpoint
	}
	endpoint, err := onvif.ParseEndpoint(raw)
	if err != nil {
		return onvif.Endpoint{}, netip.Addr{}, onvif.ErrInvalidEndpoint
	}
	u, err := url.Parse(endpoint.String())
	if err != nil || u == nil || u.Hostname() == "" {
		return onvif.Endpoint{}, netip.Addr{}, onvif.ErrInvalidEndpoint
	}
	address, err := netip.ParseAddr(u.Hostname())
	if err != nil || !address.Is4() {
		return onvif.Endpoint{}, netip.Addr{}, onvif.ErrInvalidEndpoint
	}
	return endpoint, address, nil
}

// NewDeviceProbe returns a read-only ONVIF probe with a no-proxy transport.
// Its injectable RoundTripper is for deterministic tests; production passes nil.
func NewDeviceProbe(transport http.RoundTripper) DeviceProber {
	if transport == nil {
		transport = &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	return func(ctx context.Context, endpoint onvif.Endpoint, username, password string) (ProbeResult, error) {
		var credentials *onvif.Credentials
		if username != "" {
			credentials = onvif.NewCredentials(username, password)
		}
		client := onvif.NewClient(transport, onvif.Config{
			Timeout:      probeOperationTimeout,
			MaxBodyBytes: 64 << 10,
			MaxAttempts:  1,
			Credentials:  credentials,
		})
		device := onvif.NewDeviceClient(client, endpoint)

		info, err := device.GetDeviceInformation(ctx)
		var clock onvif.DeviceClock
		if err != nil && credentials != nil {
			var callErr *onvif.CallError
			if errors.As(err, &callErr) && callErr.Code == "authentication_failed" {
				// Clock skew compensation: Query unauthenticated GetSystemDateAndTime to detect camera clock offset.
				unauthClient := onvif.NewClient(transport, onvif.Config{
					Timeout:      probeOperationTimeout,
					MaxBodyBytes: 64 << 10,
					MaxAttempts:  1,
				})
				unauthDevice := onvif.NewDeviceClient(unauthClient, endpoint)
				if unauthClock, clockErr := unauthDevice.GetSystemDateAndTime(ctx); clockErr == nil {
					clock = unauthClock
					if clock.UTC.Date.Year >= 2000 {
						camTime := time.Date(
							clock.UTC.Date.Year, time.Month(clock.UTC.Date.Month), clock.UTC.Date.Day,
							clock.UTC.Time.Hour, clock.UTC.Time.Minute, clock.UTC.Time.Second, 0, time.UTC,
						)
						timeOffset := camTime.Sub(time.Now().UTC())
						client.SetTimeOffset(timeOffset)
						// Retry authenticated call with compensated time offset
						info, err = device.GetDeviceInformation(ctx)
					}
				}
				// If still failing, attempt unauthenticated GetDeviceInformation as last resort
				if err != nil {
					if unauthInfo, unauthErr := unauthDevice.GetDeviceInformation(ctx); unauthErr == nil {
						info = unauthInfo
						err = nil
					}
				}
			}
		}
		if err != nil {
			return ProbeResult{}, err
		}

		// Services discovery: Try GetServices (ONVIF 2.0+), fallback to GetCapabilities (ONVIF 1.0)
		services, err := device.GetServices(ctx)
		if err != nil || len(services) == 0 {
			if capsServices, capsErr := device.GetCapabilities(ctx); capsErr == nil && len(capsServices) > 0 {
				services = capsServices
			} else if err != nil && len(services) == 0 {
				// Don't fail the probe if services enumeration fails but we got valid device information
				services = []onvif.Service{}
			}
		}

		// If clock wasn't retrieved during skew compensation, retrieve it now
		if clock.DateTimeType == "" && clock.UTC.Date.Year == 0 {
			clock, _ = device.GetSystemDateAndTime(ctx)
		}

		// Query profiles & stream URIs if media service is present
		var probeProfiles []ProbeProfile
		var mainURL, subURL string
		for _, s := range services {
			if strings.Contains(s.Namespace, "/media") {
				if mediaEp, epErr := s.ValidatedEndpoint(); epErr == nil {
					mediaClient := onvif.NewMediaClient(client, mediaEp)
					if onvifProfiles, profErr := mediaClient.GetProfiles(ctx); profErr == nil {
						for _, prof := range onvifProfiles {
							p := ProbeProfile{
								Token: prof.Token,
								Name:  prof.Name,
							}
							if prof.VideoEncoderConfiguration != nil {
								p.Encoding = prof.VideoEncoderConfiguration.Encoding
								p.Width = prof.VideoEncoderConfiguration.Resolution.Width
								p.Height = prof.VideoEncoderConfiguration.Resolution.Height
								p.FPS = prof.VideoEncoderConfiguration.RateControl.FrameRateLimit
								p.Bitrate = prof.VideoEncoderConfiguration.RateControl.BitrateLimit
							}
							if uri, uriErr := mediaClient.GetStreamURI(ctx, prof.Token); uriErr == nil {
								p.StreamURI = uri
							}
							probeProfiles = append(probeProfiles, p)
						}
					}
				}
				break
			}
		}

		// Determine Main and Sub streams
		for _, p := range probeProfiles {
			if p.StreamURI == "" {
				continue
			}
			if mainURL == "" {
				mainURL = p.StreamURI
			} else if p.Width*p.Height > 0 {
				for _, m := range probeProfiles {
					if m.StreamURI == mainURL {
						if p.Width*p.Height > m.Width*m.Height {
							mainURL = p.StreamURI
						}
						break
					}
				}
			}
		}
		for _, p := range probeProfiles {
			if p.StreamURI != "" && p.StreamURI != mainURL {
				if subURL == "" {
					subURL = p.StreamURI
				}
			}
		}

		// PTZ inquiry
		var ptz ProbePTZ
		for _, s := range services {
			if strings.Contains(s.Namespace, "/ptz") {
				ptz.Supported = true
				if ptzEp, epErr := s.ValidatedEndpoint(); epErr == nil && len(probeProfiles) > 0 {
					ptzClient := onvif.NewPTZClient(client, ptzEp)
					if presets, presetErr := ptzClient.GetPresets(ctx, probeProfiles[0].Token); presetErr == nil {
						ptz.Presets = presets
					}
				}
				break
			}
		}

		// Analytics inquiry
		var analytics ProbeAnalytics
		for _, s := range services {
			if strings.Contains(s.Namespace, "/events") || strings.Contains(s.Namespace, "/analytics") {
				if evEp, epErr := s.ValidatedEndpoint(); epErr == nil {
					evClient := onvif.NewEventsClient(client, evEp)
					if props, propErr := evClient.GetEventProperties(ctx); propErr == nil {
						analytics.SupportedTopics = props.TopicSet
					}
				}
				break
			}
		}

		return ProbeResult{
			Information: info,
			Services:    services,
			Clock:       clock,
			Profiles:    probeProfiles,
			Streams:     ProbeStreams{MainURL: mainURL, SubURL: subURL},
			Analytics:   analytics,
			PTZ:         ptz,
		}, nil
	}
}

func projectProbeResult(result ProbeResult, networks []netip.Prefix, agentToken, username, password string) probeResponse {
	out := probeResponse{
		Information: probeDeviceInformation{
			Manufacturer:    sanitizeProbeText(result.Information.Manufacturer, agentToken, username, password),
			Model:           sanitizeProbeText(result.Information.Model, agentToken, username, password),
			FirmwareVersion: sanitizeProbeText(result.Information.FirmwareVersion, agentToken, username, password),
			SerialNumber:    sanitizeProbeText(result.Information.SerialNumber, agentToken, username, password),
			HardwareID:      sanitizeProbeText(result.Information.HardwareID, agentToken, username, password),
		},
		Services:  make([]probeService, 0, min(len(result.Services), maxProbeServices)),
		Clock:     projectClock(result.Clock, agentToken, username, password),
		Profiles:  result.Profiles,
		Streams:   result.Streams,
		Analytics: result.Analytics,
		PTZ:       result.PTZ,
	}
	for _, service := range result.Services {
		if len(out.Services) >= maxProbeServices {
			break
		}
		version := service.Version
		if version.Major < 0 || version.Major > 65535 {
			version.Major = 0
		}
		if version.Minor < 0 || version.Minor > 65535 {
			version.Minor = 0
		}
		projected := probeService{Namespace: sanitizeProbeText(service.Namespace, agentToken, username, password), Version: probeVersion{Major: version.Major, Minor: version.Minor}, XAddrs: make([]string, 0, 1)}
		if endpoint, ok := projectServiceEndpoint(service.Endpoint, networks); ok {
			projected.XAddrs = append(projected.XAddrs, sanitizeProbeText(endpoint, agentToken, username, password))
		}
		if len(projected.XAddrs) > 0 {
			out.Services = append(out.Services, projected)
		}
	}
	return out
}

func projectServiceEndpoint(raw string, networks []netip.Prefix) (string, bool) {
	_, address, err := parseProbeEndpoint(raw)
	if err != nil {
		return "", false
	}
	allowed := false
	for _, prefix := range networks {
		if prefix.Contains(address) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", false
	}
	// Device-provided XAddrs are reported as origins only and are never followed.
	u, _ := url.Parse(raw)
	port := u.Port()
	if port == "" {
		return u.Scheme + "://" + address.String(), true
	}
	return u.Scheme + "://" + net.JoinHostPort(address.String(), port), true
}

func projectClock(clock onvif.DeviceClock, agentToken, username, password string) probeClock {
	return probeClock{DateTimeType: sanitizeProbeText(clock.DateTimeType, agentToken, username, password), UTC: projectDateTime(clock.UTC), Local: projectDateTime(clock.Local)}
}

func projectDateTime(value onvif.DateTimeValue) probeDateTime {
	if value.Date.Year < 1970 || value.Date.Year > 9999 {
		value.Date.Year = 0
	}
	if value.Date.Month < 1 || value.Date.Month > 12 {
		value.Date.Month = 0
	}
	if value.Date.Day < 1 || value.Date.Day > 31 {
		value.Date.Day = 0
	}
	if value.Time.Hour < 0 || value.Time.Hour > 23 {
		value.Time.Hour = 0
	}
	if value.Time.Minute < 0 || value.Time.Minute > 59 {
		value.Time.Minute = 0
	}
	if value.Time.Second < 0 || value.Time.Second > 59 {
		value.Time.Second = 0
	}
	return probeDateTime{
		Time: probeTime{Hour: value.Time.Hour, Minute: value.Time.Minute, Second: value.Time.Second},
		Date: probeDate{Year: value.Date.Year, Month: value.Date.Month, Day: value.Date.Day},
	}
}

func sanitizeProbeText(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = strings.ToValidUTF8(value, "�")
	var out strings.Builder
	count := 0
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if count >= maxProbeFieldRunes {
			break
		}
		out.WriteRune(r)
		count++
	}
	return out.String()
}
