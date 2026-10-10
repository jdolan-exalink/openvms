// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"net/netip"
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
	// LoginMaxFailures failed logins in a row lock the account for LoginLockout.
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

	// WorkerURL is the worker probe address. The API reads ONNX throughput from it.
	WorkerURL string
}

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
		TrustForwardedFor:           boolean("TRUST_FORWARDED_FOR", false),
		HealthInterval:              duration("HEALTH_INTERVAL", 30*time.Second),
		EventSyncInterval:           duration("EVENT_SYNC_INTERVAL", 5*time.Second),
		EventBackfill:               duration("EVENT_BACKFILL", 24*time.Hour),
		SessionTTL:                  duration("SESSION_TTL", 12*time.Hour),
		SessionIdle:                 duration("SESSION_IDLE", 2*time.Hour),
		LoginMaxFailures:            integer("LOGIN_MAX_FAILURES", 5),
		LoginLockout:                duration("LOGIN_LOCKOUT", 15*time.Minute),
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
