package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/envutil"
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
	TLSEnabled           bool
	TLSCertPath          string
	TLSKeyPath           string
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
}

func Load() (*Config, error) {
	env := envutil.String("ENV", "production")
	allowLocalhostInProd := envutil.Bool("ALLOW_LOCALHOST_IN_PROD", false)
	enableSwagger := envutil.Bool("ENABLE_SWAGGER_UI", env != "production")
	enableOpenAPI := envutil.Bool("ENABLE_OPENAPI", enableSwagger || env != "production")

	adminHash, sessionSecret, err := loadCredentials()
	if err != nil {
		return nil, err
	}

	port, err := parsePort(envutil.Int("PORT", 30190))
	if err != nil {
		return nil, err
	}
	valkeyURL, err := validateValkeyURL(envutil.String("VALKEY_URL", "valkey-cache:6379"))
	if err != nil {
		return nil, err
	}
	sessionCfg, err := LoadSessionConfig()
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:              port,
		Env:               env,
		AdminUser:         envutil.String("ADMIN_USER", "admin"),
		AdminPassHash:     adminHash,
		SessionSecret:     sessionSecret,
		ValkeyURL:         valkeyURL,
		DockerHost:        envutil.String("DOCKER_HOST", "tcp://docker-proxy:2375"),
		HoloAdminAPIURL:   aliasOrDefault("http://hololive-admin-api:30006", "HOLO_ADMIN_API_URL", "HOLO_BOT_URL"),
		HoloBotAPIKey:     aliasOrDefault("", "HOLO_BOT_API_KEY", "API_SECRET_KEY"),
		EnableOpenAPI:     enableOpenAPI,
		EnableSwaggerUI:   enableSwagger,
		Logging:           LoadLoggingConfig(),
		Security:          LoadSecurityConfig(env, allowLocalhostInProd),
		Session:           sessionCfg,
		RuntimeVersion:    envutil.String("ADMIN_DASHBOARD_VERSION", "0.1.0-go"),
		TrustedForwarders: envutil.Bool("TRUST_FORWARDED_HEADERS", false),
	}, nil
}

func LoadLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      envutil.String("LOG_LEVEL", "info"),
		Dir:        envutil.String("LOG_DIR", ""),
		MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 10),
		MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 14),
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
		return "", "", err
	}
	adminHash = normalizeEscapedBcryptHash(adminHash)
	if err := bcrypt.CompareHashAndPassword([]byte(adminHash), []byte("")); err != nil && !isBcryptPasswordMismatch(err) {
		return "", "", fmt.Errorf("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt hash: %w", err)
	}
	sessionSecret, err = requiredAlias("SESSION_SECRET", "ADMIN_SECRET_KEY")
	if err != nil {
		return "", "", err
	}
	if len(sessionSecret) < 16 {
		return "", "", fmt.Errorf("SESSION_SECRET or ADMIN_SECRET_KEY must be at least 16 bytes")
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
	err := (&cfg).Validate()
	return cfg, err
}

func (c *SessionConfig) Validate() error {
	if c.HeartbeatInterval < time.Second {
		return fmt.Errorf("SESSION_HEARTBEAT_INTERVAL_MS must be at least 1000")
	}
	if c.ExpiryDuration < time.Minute {
		return fmt.Errorf("session expiry_duration must be at least 60 seconds")
	}
	if c.AbsoluteTimeout <= c.ExpiryDuration {
		return fmt.Errorf("session absolute_timeout must be greater than expiry_duration")
	}
	if c.IdleTimeout < time.Minute {
		return fmt.Errorf("SESSION_IDLE_TIMEOUT_MS must be at least 60000")
	}
	if c.IdleWarningTimeout >= c.IdleTimeout {
		return fmt.Errorf("SESSION_IDLE_WARNING_TIMEOUT_MS must be less than SESSION_IDLE_TIMEOUT_MS")
	}
	return c.validateTTLWindows()
}

func (c *SessionConfig) validateTTLWindows() error {
	if c.IdleSessionTTL < time.Second {
		return fmt.Errorf("idle_session_ttl must be at least 1 second")
	}
	if c.IdleSessionTTL >= c.IdleTimeout {
		return fmt.Errorf("idle_session_ttl must be less than idle_timeout")
	}
	if c.AbsoluteWarningWindow >= c.AbsoluteTimeout {
		return fmt.Errorf("SESSION_ABSOLUTE_WARNING_WINDOW_MS must be less than absolute_timeout")
	}
	if c.RotationInterval < c.GracePeriod {
		return fmt.Errorf("rotation_interval must be greater than or equal to grace_period")
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
		TLSEnabled:           envutil.Bool("TLS_ENABLED", false),
		TLSCertPath:          envutil.String("TLS_CERT_PATH", "/certs/localhost.crt"),
		TLSKeyPath:           envutil.String("TLS_KEY_PATH", "/certs/localhost.key"),
	}
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

func configuredOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
	if raw == "" {
		return fallbackOrigins()
	}
	origins := make([]string, 0, 4)
	for item := range strings.SplitSeq(raw, ",") {
		origin := normalizeOrigin(item)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func dropLocalhostOrigins(origins []string) []string {
	filtered := origins[:0]
	for _, origin := range origins {
		if !isLocalhostOrigin(origin) {
			filtered = append(filtered, origin)
		}
	}
	return filtered
}

func fallbackOrigins() []string {
	return []string{
		"https://admin.capu.blog",
		"http://localhost:5173",
		"http://localhost:30190",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:30190",
	}
}

func normalizeOrigin(origin string) string {
	return strings.TrimRight(strings.TrimSpace(origin), "/")
}

func isLocalhostOrigin(origin string) bool {
	normalized := strings.ToLower(normalizeOrigin(origin))
	authority := normalized
	if parts := strings.SplitN(normalized, "://", 2); len(parts) == 2 {
		authority = parts[1]
	}
	if strings.HasPrefix(authority, "[") {
		end := strings.Index(authority, "]")
		if end >= 0 {
			return authority[:end+1] == "[::1]"
		}
	}
	host := strings.Split(authority, ":")[0]
	return host == "localhost" || host == "127.0.0.1"
}

func millisEnv(key string, fallback time.Duration) time.Duration {
	return time.Duration(envutil.Int(key, int(fallback.Milliseconds()))) * time.Millisecond
}

func requiredAlias(keys ...string) (string, error) {
	if value := envutil.StringAny(keys...); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("required environment variable missing: %s", strings.Join(keys, " or "))
}

func parsePort(port int) (uint16, error) {
	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("PORT=%d is out of u16 range", port)
	}
	return uint16(port), nil
}

func normalizeEscapedBcryptHash(hash string) string {
	if strings.HasPrefix(hash, "$$2a$$") || strings.HasPrefix(hash, "$$2b$$") || strings.HasPrefix(hash, "$$2y$$") {
		return strings.ReplaceAll(hash, "$$", "$")
	}
	return hash
}

func isBcryptPasswordMismatch(err error) bool {
	return errors.Is(err, bcrypt.ErrMismatchedHashAndPassword)
}

func validateValkeyURL(value string) (string, error) {
	if strings.Contains(value, "://") {
		return "", fmt.Errorf("VALKEY_URL must not include a URL scheme; configure host:port or :urlencoded_password@host:port")
	}
	if userinfo, _, ok := strings.Cut(value, "@"); ok && userinfo != "" {
		if strings.ContainsAny(userinfo, " #?/\\") {
			return "", fmt.Errorf("VALKEY_URL userinfo contains unsafe characters; URL-encode the password or use a safe secret value")
		}
	}
	return value, nil
}
