package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"golang.org/x/crypto/bcrypt"
)

func validateAllowedOrigins(env string, origins []string) error {
	if strings.EqualFold(env, "production") && len(origins) == 0 {
		return errors.New("config: ALLOWED_ORIGINS must contain at least one permitted origin in production")
	}

	return nil
}

func configuredOrigins() []string {
	raw := envutil.String("ALLOWED_ORIGINS", "")
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
		return "", errors.New("VALKEY_URL must not include a URL scheme; configure host:port or :urlencoded_password@host:port")
	}

	if userinfo, _, ok := strings.Cut(value, "@"); ok && userinfo != "" {
		if strings.ContainsAny(userinfo, " #?/\\") {
			return "", errors.New("VALKEY_URL userinfo contains unsafe characters; URL-encode the password or use a safe secret value")
		}
	}

	return value, nil
}
