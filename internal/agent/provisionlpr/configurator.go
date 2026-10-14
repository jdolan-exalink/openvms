package provisionlpr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CameraConfigParams defines parameters to configure a Provision-ISR camera to report to the agent.
type CameraConfigParams struct {
	CameraIP       string `json:"camera_ip"`
	CameraPort     int    `json:"camera_port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	TargetHost     string `json:"target_host"`
	TargetPort     int    `json:"target_port"`
	KeepalivePath  string `json:"keepalive_path"`
	EventPath      string `json:"event_path"`
	IntervalSecond int    `json:"interval_second"`
}

// ConfigResult contains the outcome of an automated or manual configuration attempt.
type ConfigResult struct {
	Success        bool   `json:"success"`
	Method         string `json:"method"` // "cgi_api", "manual_instructions"
	Message        string `json:"message"`
	WebUIPath      string `json:"web_ui_path"`
	Parameters     map[string]string `json:"parameters"`
}

// Configurator provides automated push and guided configuration for Provision-ISR cameras.
type Configurator struct {
	client *http.Client
}

// NewConfigurator creates a new Configurator.
func NewConfigurator() *Configurator {
	return &Configurator{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Configure attempts to apply the HTTP POST configuration to the camera via CGI,
// and returns full configuration details and manual steps.
func (c *Configurator) Configure(ctx context.Context, p CameraConfigParams) ConfigResult {
	if p.CameraPort <= 0 {
		p.CameraPort = 80
	}
	if p.TargetPort <= 0 {
		p.TargetPort = 7419
	}
	if p.KeepalivePath == "" {
		p.KeepalivePath = "/SendKeepalive"
	}
	if p.EventPath == "" {
		p.EventPath = "/v1/provision-lpr/event"
	}
	if p.IntervalSecond <= 0 {
		p.IntervalSecond = 20
	}

	params := map[string]string{
		"Server IP / Domain":   p.TargetHost,
		"Server Port":          strconv.Itoa(p.TargetPort),
		"Keepalive Endpoint":   p.KeepalivePath,
		"LPR Event Endpoint":   p.EventPath,
		"Heartbeat Interval":   fmt.Sprintf("%d s", p.IntervalSecond),
		"Protocol / Mechanism": "HTTP POST / API",
	}

	result := ConfigResult{
		WebUIPath:  "Configuración -> Red -> Configuración de Servidor -> HTTP POST / API",
		Parameters: params,
	}

	// Try common Provision-ISR / TVT CGI endpoint for HTTP POST configuration
	if p.CameraIP != "" {
		endpoint := fmt.Sprintf("http://%s:%d/http/post.cgi", p.CameraIP, p.CameraPort)
		form := url.Values{
			"action":    {"set"},
			"enable":    {"1"},
			"server_ip": {p.TargetHost},
			"port":      {strconv.Itoa(p.TargetPort)},
			"interval":  {strconv.Itoa(p.IntervalSecond)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if p.Username != "" {
				req.SetBasicAuth(p.Username, p.Password)
			}
			resp, err := c.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					result.Success = true
					result.Method = "cgi_api"
					result.Message = "Configuración HTTP POST aplicada exitosamente a la cámara vía API"
					return result
				}
			}
		}
	}

	result.Success = true
	result.Method = "manual_instructions"
	result.Message = "Parámetros preparados para configuración en la interfaz Web de la cámara Provision-ISR"
	return result
}
