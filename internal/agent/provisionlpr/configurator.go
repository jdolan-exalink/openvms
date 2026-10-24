package provisionlpr

import (
	"context"
	"fmt"
	"strconv"
)

// CameraConfigParams defines what a Provision-ISR camera has to be told to report to the agent.
// Username, Password, CameraIP and CameraPort are accepted for compatibility with older callers and
// are not used: the configurator never contacts the camera.
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

// ConfigResult is the outcome of preparing a camera's configuration.
type ConfigResult struct {
	// Success is true only when the camera is known to report to this agent. The configurator cannot
	// know that by itself, so it never sets it: the caller verifies it from the camera's keepalives.
	Success    bool              `json:"success"`
	Method     string            `json:"method"` // "manual_instructions"
	Message    string            `json:"message"`
	WebUIPath  string            `json:"web_ui_path"`
	Parameters map[string]string `json:"parameters"`
}

// Configurator prepares the guided configuration of Provision-ISR cameras.
//
// The manufacturer's document describes one way to point a camera at a server: the HTTP POST screen
// of its web interface. It documents no way to set that remotely, so the configurator does not try
// one (it used to post to a guessed CGI and report success); it returns the values to enter.
type Configurator struct{}

// NewConfigurator creates a Configurator.
func NewConfigurator() *Configurator { return &Configurator{} }

// Configure returns the values to enter in the camera's HTTP POST screen, filling in the defaults.
// It makes no network call and never reports success.
func (c *Configurator) Configure(_ context.Context, p CameraConfigParams) ConfigResult {
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
	return ConfigResult{
		Method:    "manual_instructions",
		Message:   "Ingresá estos valores en la interfaz web de la cámara. OpenVMS no puede aplicarlos solo: el fabricante no documenta una API para configurarlos. Cuando la cámara empiece a reportar, quedará verificada.",
		WebUIPath: "Configuración -> Red -> Configuración de Servidor -> HTTP POST / API",
		Parameters: map[string]string{
			"Server IP / Domain":   p.TargetHost,
			"Server Port":          strconv.Itoa(p.TargetPort),
			"Keepalive Endpoint":   p.KeepalivePath,
			"LPR Event Endpoint":   p.EventPath,
			"Heartbeat Interval":   fmt.Sprintf("%d s", p.IntervalSecond),
			"Protocol / Mechanism": "HTTP POST / API",
		},
	}
}
