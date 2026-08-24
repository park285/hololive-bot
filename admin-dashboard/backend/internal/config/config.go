package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"golang.org/x/crypto/bcrypt"
)

type SecurityMode string

const (
	SecurityEnforce SecurityMode = "enforce"
	SecurityMonitor SecurityMode = "monitor"
	SecurityOff     SecurityMode = "off"
)

type SecurityConfig struct {
	AllowedOrigins       []string
	AllowLocalhostInProd bool
	CSRFMode             SecurityMode
	WSOriginMode         SecurityMode
	ForceHTTPS           bool
}

type SessionConfig struct {
	TokenRotationEnabled  bool
	HeartbeatInterval     time.Duration
	ExpiryDuration        time.Duration
	AbsoluteTimeout       time.Duration
	AbsoluteWarningWindow time.Duration
	IdleTimeout           time.Duration
	IdleWarningTimeout    time.Duration
	IdleSessionTTL        time.Duration
	GracePeriod           time.Duration
	RotationInterval      time.Duration
}

type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

type Config struct {
	Port              uint16
	Env               string
	AdminUser         string
	AdminPassHash     string
	SessionSecret     string
	ValkeyURL         string
	DockerHost        string
	HoloAdminAPIURL   string
	HoloBotAPIKey     string
	EnableOpenAPI     bool
	EnableSwaggerUI   bool
	Logging           LoggingConfig
	Security          SecurityConfig
	Session           SessionConfig
	RuntimeVersion    string
	TrustedForwarders bool
	TrustedProxyCIDRs []netip.Prefix
}

func Load() (*Config, error) {
	env := envutil.String("ENV", "production")
	allowLocalhostInProd := envutil.Bool("ALLOW_LOCALHOST_IN_PROD", false)
	enableSwagger := envutil.Bool("ENABLE_SWAGGER_UI", env != "production")
	enableOpenAPI := envutil.Bool("ENABLE_OPENAPI", enableSwagger || env != "production")

	adminHash, sessionSecret, err := loadCredentials()
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}

	port, err := parsePort(envutil.Int("PORT", 30190))
	if err != nil {
		return nil, fmt.Errorf("parse port: %w", err)
	}

	valkeyURL, err := validateValkeyURL(envutil.String("VALKEY_URL", "valkey-cache:6379"))
	if err != nil {
		return nil, fmt.Errorf("validate valkey URL: %w", err)
	}

	sessionCfg, err := LoadSessionConfig()
	if err != nil {
		return nil, fmt.Errorf("load session config: %w", err)
	}

	trustedForwarders, trustedProxyCIDRs, err := loadTrustedProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("load trusted proxy config: %w", err)
	}

	securityCfg := LoadSecurityConfig(env, allowLocalhostInProd)
	if err := validateAllowedOrigins(env, securityCfg.AllowedOrigins); err != nil {
		return nil, fmt.Errorf("validate allowed origins: %w", err)
	}

	return &Config{
		Port:              port,
		Env:               env,
		AdminUser:         envutil.String("ADMIN_USER", "admin"),
		AdminPassHash:     adminHash,
		SessionSecret:     sessionSecret,
		ValkeyURL:         valkeyURL,
		DockerHost:        envutil.String("DOCKER_HOST", "tcp://docker-proxy:2375"),
		HoloAdminAPIURL:   aliasOrDefault("https://hololive-api:30006", "HOLO_ADMIN_API_URL", "HOLO_BOT_URL"),
		HoloBotAPIKey:     aliasOrDefault("", "HOLO_BOT_API_KEY", "API_SECRET_KEY"),
		EnableOpenAPI:     enableOpenAPI,
		EnableSwaggerUI:   enableSwagger,
		Logging:           LoadLoggingConfig(),
		Security:          securityCfg,
		Session:           sessionCfg,
		RuntimeVersion:    envutil.String("ADMIN_DASHBOARD_VERSION", "0.1.0-go"),
		TrustedForwarders: trustedForwarders,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	}, nil
}

func loadTrustedProxyConfig() (bool, []netip.Prefix, error) {
	trustedForwarders := envutil.Bool("TRUST_FORWARDED_HEADERS", false)

	trustedProxyCIDRs, err := parseTrustedProxyCIDRs(envutil.String("TRUSTED_PROXY_CIDRS", ""))
	if err != nil {
		return false, nil, fmt.Errorf("parse trusted proxy CID rs: %w", err)
	}

	if trustedForwarders && len(trustedProxyCIDRs) == 0 {
		return false, nil, errors.New("config: TRUST_FORWARDED_HEADERS is enabled but TRUSTED_PROXY_CIDRS is empty")
	}

	return trustedForwarders, trustedProxyCIDRs, nil
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	cidrs, err := httputil.ParseTrustedProxyCSV(raw)
	if err != nil {
		return nil, fmt.Errorf("config: invalid TRUSTED_PROXY_CIDRS: %w", err)
	}

	return cidrs, nil
}

func LoadLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      envutil.String("LOG_LEVEL", "info"),
		Dir:        envutil.String("LOG_DIR", ""),
		MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 5),
		MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 30),
		Compress:   envutil.Bool("LOG_COMPRESS", true),
	}
}

func aliasOrDefault(def string, keys ...string) string {
	if value := envutil.StringAny(keys...); value != "" {
		return value
	}

	return def
}

func loadCredentials() (adminHash, sessionSecret string, err error) {
	adminHash, err = requiredAlias("ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT")
	if err != nil {
		return "", "", fmt.Errorf("required alias: %w", err)
	}

	adminHash = normalizeEscapedBcryptHash(adminHash)
	if compareErr := bcrypt.CompareHashAndPassword([]byte(adminHash), []byte("")); compareErr != nil && !isBcryptPasswordMismatch(compareErr) {
		return "", "", fmt.Errorf("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt hash: %w", compareErr)
	}

	sessionSecret, err = requiredAlias("SESSION_SECRET", "ADMIN_SECRET_KEY")
	if err != nil {
		return "", "", fmt.Errorf("required alias: %w", err)
	}

	if len(sessionSecret) < 16 {
		return "", "", errors.New("SESSION_SECRET or ADMIN_SECRET_KEY must be at least 16 bytes")
	}

	return adminHash, sessionSecret, nil
}

func (c *Config) ListenAddr() string {
	return net.JoinHostPort("0.0.0.0", strconv.Itoa(int(c.Port)))
}

func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		TokenRotationEnabled:  true,
		HeartbeatInterval:     5 * time.Minute,
		ExpiryDuration:        30 * time.Minute,
		AbsoluteTimeout:       8 * time.Hour,
		AbsoluteWarningWindow: 5 * time.Minute,
		IdleTimeout:           10 * time.Minute,
		IdleWarningTimeout:    9 * time.Minute,
		IdleSessionTTL:        10 * time.Second,
		GracePeriod:           30 * time.Second,
		RotationInterval:      15 * time.Minute,
	}
}

func LoadSessionConfig() (SessionConfig, error) {
	defaults := DefaultSessionConfig()
	cfg := defaults

	cfg.TokenRotationEnabled = envutil.Bool("SESSION_TOKEN_ROTATION", true)
	cfg.HeartbeatInterval = millisEnv("SESSION_HEARTBEAT_INTERVAL_MS", defaults.HeartbeatInterval)
	cfg.AbsoluteWarningWindow = millisEnv("SESSION_ABSOLUTE_WARNING_WINDOW_MS", defaults.AbsoluteWarningWindow)
	cfg.IdleTimeout = millisEnv("SESSION_IDLE_TIMEOUT_MS", defaults.IdleTimeout)
	cfg.IdleWarningTimeout = millisEnv("SESSION_IDLE_WARNING_TIMEOUT_MS", defaults.IdleWarningTimeout)

	if err := (&cfg).Validate(); err != nil {
		return cfg, fmt.Errorf("validate session config: %w", err)
	}

	return cfg, nil
}

func (c *SessionConfig) Validate() error {
	if c.HeartbeatInterval < time.Second {
		return errors.New("SESSION_HEARTBEAT_INTERVAL_MS must be at least 1000")
	}

	if c.ExpiryDuration < time.Minute {
		return errors.New("session expiry_duration must be at least 60 seconds")
	}

	if c.AbsoluteTimeout <= c.ExpiryDuration {
		return errors.New("session absolute_timeout must be greater than expiry_duration")
	}

	if c.IdleTimeout < time.Minute {
		return errors.New("SESSION_IDLE_TIMEOUT_MS must be at least 60000")
	}

	if c.IdleWarningTimeout >= c.IdleTimeout {
		return errors.New("SESSION_IDLE_WARNING_TIMEOUT_MS must be less than SESSION_IDLE_TIMEOUT_MS")
	}

	if err := c.validateTTLWindows(); err != nil {
		return fmt.Errorf("validate TTL windows: %w", err)
	}

	return nil
}

func (c *SessionConfig) validateTTLWindows() error {
	if c.IdleSessionTTL < time.Second {
		return errors.New("idle_session_ttl must be at least 1 second")
	}

	if c.IdleSessionTTL >= c.IdleTimeout {
		return errors.New("idle_session_ttl must be less than idle_timeout")
	}

	if c.AbsoluteWarningWindow >= c.AbsoluteTimeout {
		return errors.New("SESSION_ABSOLUTE_WARNING_WINDOW_MS must be less than absolute_timeout")
	}

	if c.RotationInterval < c.GracePeriod {
		return errors.New("rotation_interval must be greater than or equal to grace_period")
	}

	if c.RotationInterval >= c.ExpiryDuration {
		return errors.New("rotation_interval must be less than expiry_duration")
	}

	return nil
}

func LoadSecurityConfig(env string, allowLocalhostInProd bool) SecurityConfig {
	return SecurityConfig{
		AllowedOrigins:       parseAllowedOrigins(env, allowLocalhostInProd),
		AllowLocalhostInProd: allowLocalhostInProd,
		CSRFMode:             parseSecurityMode(envutil.String("CSRF_MODE", string(SecurityEnforce))),
		WSOriginMode:         parseSecurityMode(envutil.String("WS_ORIGIN_MODE", string(SecurityEnforce))),
		ForceHTTPS:           envutil.Bool("FORCE_HTTPS", true),
	}
}

func (c *Config) ForwardedTrustWarning() string {
	if c.Security.ForceHTTPS && !c.TrustedForwarders {
		return "FORCE_HTTPS is on but TRUST_FORWARDED_HEADERS is off: behind a reverse proxy every client resolves to the proxy IP, so the login rate limiter shares one bucket and a scanner can lock out real admins; set TRUST_FORWARDED_HEADERS and TRUSTED_PROXY_CIDRS"
	}

	return ""
}

func parseSecurityMode(value string) SecurityMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(SecurityMonitor):
		return SecurityMonitor
	case string(SecurityOff):
		return SecurityOff
	default:
		return SecurityEnforce
	}
}

func parseAllowedOrigins(env string, allowLocalhostInProd bool) []string {
	origins := configuredOrigins()

	if strings.EqualFold(env, "production") && !allowLocalhostInProd {
		return dropLocalhostOrigins(origins)
	}

	return origins
}
