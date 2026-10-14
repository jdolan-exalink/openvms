// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Service  string
	LogLevel string
	HTTPAddr string
	GRPCAddr string
	// GRPCTLSCertFile and GRPCTLSKeyFile enable TLS on the gRPC control channel; both or neither.
	GRPCTLSCertFile string
	GRPCTLSKeyFile  string
	// GRPCAgentAddr is the listen address of the separate agent gRPC listener, which requires
	// a client certificate issued by the agent CA. Empty (the default) disables it; setting it
	// requires server TLS (GRPC_TLS_CERT_FILE / GRPC_TLS_KEY_FILE).
	GRPCAgentAddr string
	// AgentEnroll tells SSH installs how agents reach the API; see AgentEnrollConfig.
	AgentEnroll AgentEnrollConfig

	DatabaseURL    string
	MigrateOnStart bool

	ValkeyAddr string
	NATSURL    string

	S3 S3Config

	// Features are rollout flags (OPENVMS_FEATURES, comma-separated), all off by default.
	Features Features

	// Waha is the internal WhatsApp HTTP API (devlikeapro/waha) used by WhatsApp channels.
	Waha WahaConfig

	// TrustForwardedFor takes the client IP from X-Forwarded-For (API behind Caddy).
	TrustForwardedFor bool
	// CredentialTrustedProxyCIDRs is the explicit peer allowlist for forwarded HTTPS on credential-bearing routes.
	CredentialTrustedProxyCIDRs []netip.Prefix
	// HealthInterval is how often the worker polls each Frigate server.
	HealthInterval time.Duration
	// NoSignalHold is how long a Frigate camera must report camera_fps ~0 before it is
	// marked without signal and live view is refused (default two health polls).
	NoSignalHold time.Duration
	// EventSyncInterval is how often the worker pulls review items from each Frigate.
	EventSyncInterval time.Duration
	// EventBackfill is how far back a newly registered Frigate is imported.
	EventBackfill time.Duration

	// NotifyDeliveryRetention and NotifyReadRetention bound how long terminal notification
	// deliveries and read in-app notifications are kept; a negative value disables pruning.
	NotifyDeliveryRetention time.Duration
	NotifyReadRetention     time.Duration

	// SessionTTL and SessionIdle bound browser sessions (absolute and without activity).
	SessionTTL  time.Duration
	SessionIdle time.Duration
	// LoginMaxFailures failed logins in a row lock the account for LoginLockout; zero disables account lockout.
	LoginMaxFailures int
	LoginLockout     time.Duration

	// Live* tune the live-view websocket gateway (internal/media).
	LiveAuditWindow        time.Duration
	LiveRevalidateInterval time.Duration
	LivePingInterval       time.Duration
	LivePongWait           time.Duration

	ShutdownTimeout time.Duration

	// Export storage and bandwidth limits (internal/media).
	ExportStoragePath         string
	ExportGlobalBandwidthMbps int
	ExportServerBandwidthMbps int

	// AdminUsername and AdminPassword (OPENVMS_ADMIN_USERNAME / OPENVMS_ADMIN_PASSWORD) seed the
	// first platform admin on a fresh install. An empty password makes the API generate and log a random one.
	AdminUsername string
	AdminPassword string
	// DefaultTenant and DefaultSite (OPENVMS_DEFAULT_TENANT / OPENVMS_DEFAULT_SITE) name the
	// tenant and site created when the install has no tenant at all.
	DefaultTenant string
	DefaultSite   string
	// DefaultSiteTZ (OPENVMS_DEFAULT_SITE_TZ) is the IANA timezone of that site.
	DefaultSiteTZ string

	// WorkerURL is the worker probe address. The API reads ONNX throughput from it.
	WorkerURL string
}

// AgentEnrollConfig is how an agent host reaches the API, used when the API installs an agent
// over SSH with mutual TLS. The API cannot know its own public address, so it is explicit. All
// empty (the default) leaves SSH installs exactly as before; GRPCAddr and APIURL are set together
// and require the agent listener (GRPC_AGENT_ADDR).
type AgentEnrollConfig struct {
	// GRPCAddr (AGENT_GRPC_PUBLIC_ADDR) is host:port of the agent listener as the agent host sees it.
	GRPCAddr string
	// APIURL (AGENT_API_PUBLIC_URL) is the https base URL of the API as the agent host sees it.
	APIURL string
	// GRPCCAFile (AGENT_GRPC_CA_FILE) is a PEM bundle on the API host that signed the agent
	// listener's server certificate, copied to agents; empty makes agents use their system roots.
	GRPCCAFile string
	// ServerName (AGENT_GRPC_SERVER_NAME) overrides the name agents verify on that certificate.
	ServerName string
	// APICAFile (AGENT_API_CA_FILE) is the same for the API's HTTPS certificate.
	APICAFile string
}

// Enabled reports whether SSH installs enroll agents for mutual TLS.
func (a AgentEnrollConfig) Enabled() bool { return a.GRPCAddr != "" && a.APIURL != "" }

// WahaConfig locates the WAHA service. An empty BaseURL disables WhatsApp channels.
type WahaConfig struct {
	BaseURL string
	APIKey  string
}

type S3Config struct {
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	Bucket       string
	UsePathStyle bool
}

// Load reads the environment. service names the binary for logs and traces.
func Load(service string) (Config, error) {
	trustedProxyCIDRs, err := parseCredentialTrustedProxyCIDRs(os.Getenv("CREDENTIAL_TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return Config{}, fmt.Errorf("CREDENTIAL_TRUSTED_PROXY_CIDRS is invalid: %w", err)
	}
	c := Config{
		CredentialTrustedProxyCIDRs: trustedProxyCIDRs,
		Service:                     service,
		LogLevel:                    str("LOG_LEVEL", "info"),
		HTTPAddr:                    str("HTTP_ADDR", ":8080"),
		GRPCAddr:                    str("GRPC_ADDR", ":9090"),
		GRPCTLSCertFile:             str("GRPC_TLS_CERT_FILE", ""),
		GRPCTLSKeyFile:              str("GRPC_TLS_KEY_FILE", ""),
		GRPCAgentAddr:               str("GRPC_AGENT_ADDR", ""),
		DatabaseURL:                 str("DATABASE_URL", ""),
		MigrateOnStart:              boolean("MIGRATE_ON_START", true),
		ValkeyAddr:                  str("VALKEY_ADDR", "localhost:6379"),
		NATSURL:                     str("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout:             duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		WorkerURL:                   str("WORKER_URL", "http://worker:8081"),
		AdminUsername:               str("OPENVMS_ADMIN_USERNAME", "admin"),
		AdminPassword:               str("OPENVMS_ADMIN_PASSWORD", ""),
		DefaultTenant:               str("OPENVMS_DEFAULT_TENANT", "openvms"),
		DefaultSite:                 str("OPENVMS_DEFAULT_SITE", "Sitio principal"),
		DefaultSiteTZ:               str("OPENVMS_DEFAULT_SITE_TZ", "America/Argentina/Cordoba"),
		TrustForwardedFor:           boolean("TRUST_FORWARDED_FOR", false),
		HealthInterval:              duration("HEALTH_INTERVAL", 30*time.Second),
		NoSignalHold:                duration("CAMERA_NO_SIGNAL_HOLD", 60*time.Second),
		EventSyncInterval:           duration("EVENT_SYNC_INTERVAL", 5*time.Second),
		EventBackfill:               duration("EVENT_BACKFILL", 24*time.Hour),
		SessionTTL:                  duration("SESSION_TTL", 12*time.Hour),
		SessionIdle:                 duration("SESSION_IDLE", 2*time.Hour),
		LoginMaxFailures:            nonNegativeInteger("LOGIN_MAX_FAILURES", 5),
		LoginLockout:                positiveDuration("LOGIN_LOCKOUT", 15*time.Minute),
		LiveAuditWindow:             duration("LIVE_AUDIT_WINDOW", 5*time.Minute),
		LiveRevalidateInterval:      duration("LIVE_REVALIDATE_INTERVAL", 30*time.Second),
		LivePingInterval:            duration("LIVE_PING_INTERVAL", 20*time.Second),
		LivePongWait:                duration("LIVE_PONG_WAIT", 60*time.Second),
		NotifyDeliveryRetention:     duration("NOTIFY_DELIVERY_RETENTION", 30*24*time.Hour),
		NotifyReadRetention:         duration("NOTIFY_READ_RETENTION", 90*24*time.Hour),
		ExportStoragePath:           str("EXPORT_STORAGE_PATH", "/mnt/openvms/exports"),
		ExportGlobalBandwidthMbps:   integer("EXPORT_GLOBAL_BANDWIDTH_MBPS", 50),
		ExportServerBandwidthMbps:   integer("EXPORT_SERVER_BANDWIDTH_MBPS", 10),
		Waha: WahaConfig{
			BaseURL: str("WAHA_BASE_URL", "http://waha:3000"),
			APIKey:  str("WAHA_API_KEY", ""),
		},
		Features: ParseFeatures(os.Getenv("OPENVMS_FEATURES")),
		S3: S3Config{
			Endpoint:     str("S3_ENDPOINT", "http://localhost:8333"),
			Region:       str("S3_REGION", "us-east-1"),
			AccessKey:    str("S3_ACCESS_KEY", ""),
			SecretKey:    str("S3_SECRET_KEY", ""),
			Bucket:       str("S3_BUCKET", "openvms"),
			UsePathStyle: boolean("S3_USE_PATH_STYLE", true),
		},
	}
	if (c.GRPCTLSCertFile == "") != (c.GRPCTLSKeyFile == "") {
		return c, fmt.Errorf("GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE must be set together")
	}
	if c.GRPCAgentAddr != "" && !c.GRPCTLSEnabled() {
		return c, fmt.Errorf("GRPC_AGENT_ADDR requires GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE")
	}
	if err := c.loadAgentEnroll(); err != nil {
		return c, err
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

// GRPCTLSEnabled reports whether the gRPC control channel should serve TLS.
func (c Config) GRPCTLSEnabled() bool {
	return c.GRPCTLSCertFile != "" && c.GRPCTLSKeyFile != ""
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func boolean(key string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return def
	}
	return v
}

func duration(key string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(key))
	if err != nil {
		return def
	}
	return v
}

func integer(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func nonNegativeInteger(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v < 0 || v > int(^uint32(0)>>1) {
		return def
	}
	return v
}

func positiveDuration(key string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(key))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// parseCredentialTrustedProxyCIDRs parses an explicit, fail-closed allowlist.
// Empty configuration means no forwarded scheme headers are trusted.
func parseCredentialTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]netip.Prefix, 0, len(parts))
	seen := make(map[netip.Prefix]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty CIDR entry")
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid CIDR entry")
		}
		prefix = prefix.Masked()
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	return out, nil
}

func (c *Config) loadAgentEnroll() error {
	c.AgentEnroll = AgentEnrollConfig{
		GRPCAddr:   str("AGENT_GRPC_PUBLIC_ADDR", ""),
		APIURL:     str("AGENT_API_PUBLIC_URL", ""),
		GRPCCAFile: str("AGENT_GRPC_CA_FILE", ""),
		ServerName: str("AGENT_GRPC_SERVER_NAME", ""),
		APICAFile:  str("AGENT_API_CA_FILE", ""),
	}
	a := c.AgentEnroll
	if a == (AgentEnrollConfig{}) {
		return nil
	}
	if a.GRPCAddr == "" || a.APIURL == "" {
		return fmt.Errorf("AGENT_GRPC_PUBLIC_ADDR and AGENT_API_PUBLIC_URL must be set together (the other AGENT_* install settings need both)")
	}
	if c.GRPCAgentAddr == "" {
		return fmt.Errorf("AGENT_GRPC_PUBLIC_ADDR requires the agent listener (GRPC_AGENT_ADDR)")
	}
	host, port, err := net.SplitHostPort(a.GRPCAddr)
	if p, perr := strconv.Atoi(port); err != nil || host == "" || perr != nil || p < 1 || p > 65535 {
		return fmt.Errorf("AGENT_GRPC_PUBLIC_ADDR must be host:port")
	}
	u, err := url.Parse(a.APIURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("AGENT_API_PUBLIC_URL must be an https URL without credentials (the enrollment token is a secret)")
	}
	return nil
}
