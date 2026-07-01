// Package config loads runtime config from the env and validates it at startup;
// we'd rather crash than boot with a weak pepper or missing TLS.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// 180 days is the hard cap on retention, enforced here no matter what the client asks for.
const MaxRetention = 180 * 24 * time.Hour

type Env string

const (
	EnvDev  Env = "development"
	EnvProd Env = "production"
)

// Config is the validated application configuration.
type Config struct {
	Env       Env
	BaseURL   string // canonical public origin, e.g. https://paste.720p.red
	ShortBase string // first-party short-link origin (defaults to BaseURL)
	Listen    string // public listener address

	DatabaseURL string // empty => embedded SQLite
	SQLitePath  string // path for embedded SQLite DB file
	RedisURL    string

	ServerPepper []byte // HMAC pepper for capability tokens; must be strong

	MaxPasteBytes      int64
	MaxCiphertextBytes int64
	MaxRetention       time.Duration
	DefaultStaticRet   time.Duration
	DefaultLiveRet     time.Duration
	MaxViews           int

	EnableLive          bool
	LiveMaxParticipants int
	LiveMaxUpdateBytes  int64
	LiveMaxStorageBytes int64

	EnableShortLinks bool

	// Private automation API (kept out of the public repo; disabled by default).
	PrivateAPIEnabled    bool
	PrivateAPIListen     string
	PrivateAPIAllowCIDRs []string
	PrivateAPIRequireMTL bool
	PrivateAPITokensFile string

	TrustedProxyCIDRs []string

	StunURLs   []string
	TurnURLs   []string
	TurnSecret string

	LogLevel    string
	MetricsAddr string
	MaxInFlight int // global concurrent-request cap (load shedding)

	// relaxes startup checks; local dev only
	DevInsecure bool
}

// Load reads config from the env and validates it.
func Load() (*Config, error) {
	c := &Config{
		Env:                  Env(getenv("REDPASTE_ENV", string(EnvDev))),
		BaseURL:              getenv("REDPASTE_BASE_URL", "http://localhost:8080"),
		ShortBase:            os.Getenv("REDPASTE_SHORT_BASE_URL"),
		Listen:               getenv("REDPASTE_LISTEN_ADDR", ":8080"),
		DatabaseURL:          os.Getenv("REDPASTE_DATABASE_URL"),
		SQLitePath:           getenv("REDPASTE_SQLITE_PATH", "redpaste.db"),
		RedisURL:             os.Getenv("REDPASTE_REDIS_URL"),
		MaxPasteBytes:        getenvInt64("REDPASTE_MAX_PASTE_BYTES", 3145728),      // 3 MiB
		MaxCiphertextBytes:   getenvInt64("REDPASTE_MAX_CIPHERTEXT_BYTES", 4718592), // 4.5 MiB (3 MiB * 1.5 overhead)
		MaxRetention:         time.Duration(getenvInt("REDPASTE_MAX_RETENTION_HOURS", 4320)) * time.Hour,
		DefaultStaticRet:     time.Duration(getenvInt("REDPASTE_DEFAULT_STATIC_RETENTION_HOURS", 168)) * time.Hour,
		DefaultLiveRet:       time.Duration(getenvInt("REDPASTE_DEFAULT_LIVE_RETENTION_HOURS", 24)) * time.Hour,
		MaxViews:             getenvInt("REDPASTE_MAX_VIEWS", 10000),
		EnableLive:           getenvBool("REDPASTE_ENABLE_LIVE", true),
		LiveMaxParticipants:  getenvInt("REDPASTE_LIVE_MAX_PARTICIPANTS", 8),
		LiveMaxUpdateBytes:   getenvInt64("REDPASTE_LIVE_MAX_UPDATE_BYTES", 65536),
		LiveMaxStorageBytes:  getenvInt64("REDPASTE_LIVE_MAX_STORAGE_BYTES", 20<<20),
		EnableShortLinks:     getenvBool("REDPASTE_ENABLE_SHORT_LINKS", true),
		PrivateAPIEnabled:    getenvBool("REDPASTE_PRIVATE_API_ENABLED", false),
		PrivateAPIListen:     getenv("REDPASTE_PRIVATE_API_LISTEN_ADDR", "127.0.0.1:9090"),
		PrivateAPIAllowCIDRs: splitList(os.Getenv("REDPASTE_PRIVATE_API_ALLOWED_CIDRS")),
		PrivateAPIRequireMTL: getenvBool("REDPASTE_PRIVATE_API_REQUIRE_MTLS", false),
		PrivateAPITokensFile: os.Getenv("REDPASTE_PRIVATE_API_TOKEN_HASHES_FILE"),
		TrustedProxyCIDRs:    splitList(os.Getenv("REDPASTE_TRUSTED_PROXY_CIDRS")),
		StunURLs:             splitList(os.Getenv("REDPASTE_STUN_URLS")),
		TurnURLs:             splitList(os.Getenv("REDPASTE_TURN_URLS")),
		TurnSecret:           os.Getenv("REDPASTE_TURN_SECRET"),
		LogLevel:             getenv("REDPASTE_LOG_LEVEL", "info"),
		MetricsAddr:          os.Getenv("REDPASTE_METRICS_ADDR"),
		MaxInFlight:          getenvInt("REDPASTE_MAX_INFLIGHT", 512),
		DevInsecure:          getenvBool("REDPASTE_DEV_INSECURE", false),
	}
	c.ServerPepper = []byte(os.Getenv("REDPASTE_SERVER_PEPPER"))
	if c.ShortBase == "" {
		c.ShortBase = c.BaseURL
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.Env != EnvDev && c.Env != EnvProd {
		return fmt.Errorf("REDPASTE_ENV must be development or production, got %q", c.Env)
	}
	prod := c.Env == EnvProd

	if c.MaxRetention > MaxRetention {
		return fmt.Errorf("REDPASTE_MAX_RETENTION_HOURS exceeds hard limit of 4320 hours (180 days)")
	}
	if c.DefaultStaticRet > c.MaxRetention || c.DefaultLiveRet > c.MaxRetention {
		return fmt.Errorf("default retention exceeds configured max retention")
	}

	// A weak or missing pepper is fatal in prod.
	if len(c.ServerPepper) < 32 {
		if prod && !c.DevInsecure {
			return fmt.Errorf("REDPASTE_SERVER_PEPPER must be at least 32 bytes in production")
		}
		if len(c.ServerPepper) == 0 {
			// No pepper set. For the embedded SQLite case (the zero-config docker
			// run / make dev), persist one next to the DB so capabilities survive
			// restarts. Otherwise fall back to an ephemeral one.
			if c.UsesSQLite() {
				c.ServerPepper = loadOrCreatePepper(c.SQLitePath)
			} else {
				c.ServerPepper = devEphemeralPepper()
			}
		}
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("REDPASTE_BASE_URL is not a valid absolute URL")
	}
	if prod && u.Scheme != "https" && !c.DevInsecure {
		return fmt.Errorf("REDPASTE_BASE_URL must be https in production (set REDPASTE_DEV_INSECURE=1 only behind a trusted TLS terminator)")
	}

	if c.LiveMaxParticipants < 2 || c.LiveMaxParticipants > 16 {
		return fmt.Errorf("REDPASTE_LIVE_MAX_PARTICIPANTS must be between 2 and 16")
	}
	return nil
}

// UsesSQLite reports whether we fall back to the embedded SQLite backend.
func (c *Config) UsesSQLite() bool { return strings.TrimSpace(c.DatabaseURL) == "" }

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
