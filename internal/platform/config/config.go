// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Service  string
	LogLevel string
	HTTPAddr string

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
	c := Config{
		Service:                 service,
		LogLevel:                str("LOG_LEVEL", "info"),
		HTTPAddr:                str("HTTP_ADDR", ":8080"),
		DatabaseURL:             str("DATABASE_URL", ""),
		MigrateOnStart:          boolean("MIGRATE_ON_START", true),
		ValkeyAddr:              str("VALKEY_ADDR", "localhost:6379"),
		NATSURL:                 str("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout:         duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		WorkerURL:               str("WORKER_URL", "http://worker:8081"),
		TrustForwardedFor:       boolean("TRUST_FORWARDED_FOR", false),
		HealthInterval:          duration("HEALTH_INTERVAL", 30*time.Second),
		EventSyncInterval:       duration("EVENT_SYNC_INTERVAL", 5*time.Second),
		EventBackfill:           duration("EVENT_BACKFILL", 24*time.Hour),
		SessionTTL:              duration("SESSION_TTL", 12*time.Hour),
		SessionIdle:             duration("SESSION_IDLE", 2*time.Hour),
		LoginMaxFailures:        integer("LOGIN_MAX_FAILURES", 5),
		LoginLockout:            duration("LOGIN_LOCKOUT", 15*time.Minute),
		LiveAuditWindow:         duration("LIVE_AUDIT_WINDOW", 5*time.Minute),
		LiveRevalidateInterval:  duration("LIVE_REVALIDATE_INTERVAL", 30*time.Second),
		LivePingInterval:        duration("LIVE_PING_INTERVAL", 20*time.Second),
		LivePongWait:            duration("LIVE_PONG_WAIT", 60*time.Second),
		NotifyDeliveryRetention: duration("NOTIFY_DELIVERY_RETENTION", 30*24*time.Hour),
		NotifyReadRetention:     duration("NOTIFY_READ_RETENTION", 90*24*time.Hour),
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
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
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
