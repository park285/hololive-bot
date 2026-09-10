package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"golang.org/x/crypto/bcrypt"
)

// Validate는 입력을 변경하지 않고 인증·세션·production 접근 설정을 검증합니다.
func (c *Config) Validate() error {
	switch strings.ToLower(c.Env) {
	case "production", "development", "test":
	default:
		return errors.New("ENV must be production, development or test")
	}

	if err := validateSecurityConfig(c.Env, c.Security); err != nil {
		return fmt.Errorf("security: %w", err)
	}

	if err := c.Session.Validate(); err != nil {
		return fmt.Errorf("session: %w", err)
	}

	if err := validateValkeyURL(c.ValkeyURL); err != nil {
		return fmt.Errorf("valkey: %w", err)
	}

	if err := validateCredentials(c); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	if err := c.validateTrustedForwarders(); err != nil {
		return err
	}

	if c.Logging.MaxSizeMB < 1 || c.Logging.MaxBackups < 0 || c.Logging.MaxAgeDays < 0 {
		return errors.New("log limits must be nonnegative and LOG_MAX_SIZE_MB positive")
	}

	return nil
}

func (c *Config) validateTrustedForwarders() error {
	if c.TrustedForwarders && len(c.TrustedProxyCIDRs) == 0 {
		return errors.New("TRUST_FORWARDED_HEADERS requires TRUSTED_PROXY_CIDRS")
	}

	for _, prefix := range c.TrustedProxyCIDRs {
		if !prefix.IsValid() || prefix.Bits() == 0 {
			return errors.New("TRUSTED_PROXY_CIDRS must be explicit bounded networks")
		}
	}

	if strings.EqualFold(c.Env, "production") && !c.TrustedForwarders {
		return errors.New("production requires TRUST_FORWARDED_HEADERS and explicit proxy CIDRs")
	}

	return nil
}

func validateCredentials(c *Config) error {
	if compareErr := bcrypt.CompareHashAndPassword([]byte(c.AdminPassHash), []byte("")); compareErr != nil && !isBcryptPasswordMismatch(compareErr) {
		return errors.New("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt hash")
	}

	cost, err := bcrypt.Cost([]byte(c.AdminPassHash))
	if err != nil {
		return errors.New("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt cost")
	}

	if cost < minimumAdminPasswordBcryptCost {
		return fmt.Errorf("ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt cost must be at least %d", minimumAdminPasswordBcryptCost)
	}

	if len(c.SessionSecret) < minSessionSecretBytes {
		return fmt.Errorf("SESSION_SECRET must be at least %d bytes", minSessionSecretBytes)
	}

	return nil
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

func validateAllowedOrigins(env string, origins []string) error {
	if strings.EqualFold(env, "production") && len(origins) == 0 {
		return errors.New("config: ALLOWED_ORIGINS must contain at least one permitted origin in production")
	}

	return nil
}

func validateSecurityConfig(env string, cfg SecurityConfig) error {
	if err := validateAllowedOrigins(env, cfg.AllowedOrigins); err != nil {
		return fmt.Errorf("allowed origins: %w", err)
	}

	if !strings.EqualFold(env, "production") {
		return nil
	}

	if cfg.CSRFMode != SecurityEnforce {
		return errors.New("config: CSRF_MODE must be enforce in production")
	}

	if cfg.WSOriginMode != SecurityEnforce {
		return errors.New("config: WS_ORIGIN_MODE must be enforce in production")
	}

	if !cfg.ForceHTTPS {
		return errors.New("config: FORCE_HTTPS must be enabled in production")
	}

	return validateProductionOrigins(cfg)
}

func validateProductionOrigins(cfg SecurityConfig) error {
	for _, origin := range cfg.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || parsed.Opaque != "" || parsed.Scheme != "https" && (!cfg.AllowLocalhostInProd || !isLocalhostOrigin(origin) || parsed.Scheme != "http") {
			return errors.New("config: ALLOWED_ORIGINS must contain exact HTTPS origins")
		}
	}

	return nil
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	cidrs, err := httputil.ParseTrustedProxyCSV(raw)
	if err != nil {
		return nil, fmt.Errorf("config: invalid TRUSTED_PROXY_CIDRS: %w", err)
	}

	return cidrs, nil
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

	host, _, _ := strings.Cut(authority, ":")

	return host == "localhost" || host == "127.0.0.1"
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

func validateValkeyURL(value string) error {
	if strings.Contains(value, "://") {
		return errors.New("VALKEY_URL must not include a URL scheme; configure host:port or :urlencoded_password@host:port")
	}

	if userinfo, _, ok := strings.Cut(value, "@"); ok && userinfo != "" {
		if strings.ContainsAny(userinfo, " #?/\\") {
			return errors.New("VALKEY_URL userinfo contains unsafe characters; URL-encode the password or use a safe secret value")
		}
	}

	return nil
}
