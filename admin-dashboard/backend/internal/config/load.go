package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type inputValues struct {
	values map[string]string
	err    error
}

var configEnvKeys = []string{
	"ENV", "PORT", "ADMIN_USER", "ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT", "SESSION_SECRET", "ADMIN_SECRET_KEY",
	"VALKEY_URL", "DOCKER_HOST", "HOLO_ADMIN_API_URL", "HOLO_BOT_URL", "HOLO_BOT_API_KEY", "API_SECRET_KEY",
	"ADMIN_PASS_HASH_FILE", "SESSION_SECRET_FILE", "VALKEY_URL_FILE", "HOLO_BOT_API_KEY_FILE",
	"ALLOW_LOCALHOST_IN_PROD", "ENABLE_SWAGGER_UI", "ENABLE_OPENAPI", "ADMIN_DASHBOARD_VERSION",
	"TRUST_FORWARDED_HEADERS", "TRUSTED_PROXY_CIDRS", "CSRF_MODE", "WS_ORIGIN_MODE", "FORCE_HTTPS", "ALLOWED_ORIGINS",
	"SESSION_TOKEN_ROTATION", "SESSION_HEARTBEAT_INTERVAL_MS", "SESSION_ABSOLUTE_WARNING_WINDOW_MS", "SESSION_IDLE_TIMEOUT_MS", "SESSION_IDLE_WARNING_TIMEOUT_MS",
	"LOG_LEVEL", "LOG_DIR", "LOG_MAX_SIZE_MB", "LOG_MAX_BACKUPS", "LOG_MAX_AGE_DAYS", "LOG_COMPRESS",
}

func readInputs() *inputValues {
	in := &inputValues{values: make(map[string]string, len(configEnvKeys))}
	for _, key := range configEnvKeys {
		in.values[key] = os.Getenv(key)
	}

	return in
}

// Load는 환경 snapshot과 전용 파일 입력을 결합해 한 번 검증하며 process 환경을 변경하지 않습니다.
func Load() (*Config, error) {
	in := readInputs()
	if err := in.applySecretFiles(); err != nil {
		return nil, fmt.Errorf("load secret files: %w", err)
	}

	return load(in)
}

func load(in *inputValues) (*Config, error) {
	env := in.text("ENV", "production")
	swagger := in.boolean("ENABLE_SWAGGER_UI", !strings.EqualFold(env, "production"))
	port, portErr := parsePort(in.integer("PORT", 30190))
	cidrs, cidrErr := parseTrustedProxyCIDRs(in.text("TRUSTED_PROXY_CIDRS", ""))
	adminHash, hashErr := in.requiredAlias("ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT")
	secret, secretErr := in.requiredAlias("SESSION_SECRET", "ADMIN_SECRET_KEY")
	cfg := &Config{
		Port: port, Env: env, AdminUser: in.text("ADMIN_USER", "admin"),
		AdminPassHash: normalizeEscapedBcryptHash(adminHash), SessionSecret: secret,
		ValkeyURL: in.text("VALKEY_URL", "valkey-cache:6379"), DockerHost: in.text("DOCKER_HOST", "tcp://docker-proxy:2375"),
		HoloAdminAPIURL: in.aliasOrDefault("https://hololive-api:30006", "HOLO_ADMIN_API_URL", "HOLO_BOT_URL"),
		HoloBotAPIKey:   in.aliasOrDefault("", "HOLO_BOT_API_KEY", "API_SECRET_KEY"),
		EnableOpenAPI:   in.boolean("ENABLE_OPENAPI", swagger || !strings.EqualFold(env, "production")), EnableSwaggerUI: swagger,
		Logging: in.logging(), Session: in.session(), Security: in.security(env, in.boolean("ALLOW_LOCALHOST_IN_PROD", false)),
		RuntimeVersion:    in.text("ADMIN_DASHBOARD_VERSION", "0.1.0-go"),
		TrustedForwarders: in.boolean("TRUST_FORWARDED_HEADERS", false), TrustedProxyCIDRs: cidrs,
	}

	if err := errors.Join(in.err, portErr, cidrErr, hashErr, secretErr); err != nil {
		return nil, fmt.Errorf("parse config inputs: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (in *inputValues) text(key, fallback string) string {
	if value := strings.TrimSpace(in.values[key]); value != "" {
		return value
	}

	return fallback
}

func (in *inputValues) first(keys ...string) string {
	for _, key := range keys {
		if value := in.text(key, ""); value != "" {
			return value
		}
	}

	return ""
}

func (in *inputValues) integer(key string, fallback int) int {
	raw := in.text(key, "")
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		in.err = errors.Join(in.err, fmt.Errorf("%s must be an integer", key))
		return 0
	}

	return value
}

func (in *inputValues) boolean(key string, fallback bool) bool {
	switch strings.ToLower(in.text(key, "")) {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		in.err = errors.Join(in.err, fmt.Errorf("%s must be a boolean", key))
		return false
	}
}

func (in *inputValues) securityMode(key string) SecurityMode {
	mode := SecurityMode(strings.ToLower(in.text(key, string(SecurityEnforce))))
	switch mode {
	case SecurityEnforce, SecurityMonitor, SecurityOff:
		return mode
	default:
		in.err = errors.Join(in.err, fmt.Errorf("%s must be enforce, monitor or off", key))
		return ""
	}
}

func (in *inputValues) logging() LoggingConfig {
	return LoggingConfig{
		Level:      in.text("LOG_LEVEL", "info"),
		Dir:        in.text("LOG_DIR", ""),
		MaxSizeMB:  in.integer("LOG_MAX_SIZE_MB", 5),
		MaxBackups: in.integer("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: in.integer("LOG_MAX_AGE_DAYS", 30),
		Compress:   in.boolean("LOG_COMPRESS", true),
	}
}

func (in *inputValues) aliasOrDefault(def string, keys ...string) string {
	if value := in.first(keys...); value != "" {
		return value
	}

	return def
}

func (in *inputValues) session() SessionConfig {
	defaults := DefaultSessionConfig()
	cfg := defaults

	cfg.TokenRotationEnabled = in.boolean("SESSION_TOKEN_ROTATION", true)
	cfg.HeartbeatInterval = in.millis("SESSION_HEARTBEAT_INTERVAL_MS", defaults.HeartbeatInterval)
	cfg.AbsoluteWarningWindow = in.millis("SESSION_ABSOLUTE_WARNING_WINDOW_MS", defaults.AbsoluteWarningWindow)
	cfg.IdleTimeout = in.millis("SESSION_IDLE_TIMEOUT_MS", defaults.IdleTimeout)
	cfg.IdleWarningTimeout = in.millis("SESSION_IDLE_WARNING_TIMEOUT_MS", defaults.IdleWarningTimeout)

	return cfg
}

func (in *inputValues) security(env string, allowLocalhostInProd bool) SecurityConfig {
	return SecurityConfig{
		AllowedOrigins:       in.allowedOrigins(env, allowLocalhostInProd),
		AllowLocalhostInProd: allowLocalhostInProd,
		CSRFMode:             in.securityMode("CSRF_MODE"),
		WSOriginMode:         in.securityMode("WS_ORIGIN_MODE"),
		ForceHTTPS:           in.boolean("FORCE_HTTPS", true),
	}
}

func (in *inputValues) allowedOrigins(env string, allowLocalhostInProd bool) []string {
	origins := in.configuredOrigins()

	if strings.EqualFold(env, "production") && !allowLocalhostInProd {
		return dropLocalhostOrigins(origins)
	}

	return origins
}

func (in *inputValues) configuredOrigins() []string {
	raw := in.text("ALLOWED_ORIGINS", "")
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
		"http://localhost:5173",
		"http://localhost:30190",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:30190",
	}
}

func (in *inputValues) millis(key string, fallback time.Duration) time.Duration {
	value := in.integer(key, int(fallback.Milliseconds()))
	if value < 0 || int64(value) > math.MaxInt64/int64(time.Millisecond) {
		in.err = errors.Join(in.err, fmt.Errorf("%s is outside the duration range", key))
		return 0
	}

	return time.Duration(value) * time.Millisecond
}

func (in *inputValues) requiredAlias(keys ...string) (string, error) {
	if value := in.first(keys...); value != "" {
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
