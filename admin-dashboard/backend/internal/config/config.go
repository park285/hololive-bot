// Package config: 설정 관리
package config

import (
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

// LogConfig: 로깅 설정 타입 별칭 (shared-go SSOT)
type LogConfig = logging.Config

type Config struct {
	Port        string
	Environment string
	ForceHTTPS  bool
	Log         LogConfig

	// TLS 설정 (HTTP/2 지원)
	TLSEnabled  bool
	TLSCertPath string
	TLSKeyPath  string

	// 인증 설정
	AdminUser            string
	AdminPassHash        string
	AdminSecretKey       string
	SessionTokenRotation bool

	// Metrics 설정
	ValkeyURL  string
	DockerHost string

	// 각 봇 프록시 URL 및 API Key
	HoloBotURL    string
	HoloBotAPIKey string // hololive-bot의 X-API-Key 인증용

	// OTEL 설정
	OTELEnabled     bool
	OTELEndpoint    string
	OTELServiceName string
	OTLPInsecure    bool
}

// SessionConfig: 세션 관련 상수
var SessionConfig = struct {
	ExpiryDuration   time.Duration
	AbsoluteTimeout  time.Duration
	IdleSessionTTL   time.Duration
	GracePeriod      time.Duration
	RotationInterval time.Duration
}{
	ExpiryDuration:   30 * time.Minute,
	AbsoluteTimeout:  8 * time.Hour,
	IdleSessionTTL:   10 * time.Second,
	GracePeriod:      30 * time.Second,
	RotationInterval: 15 * time.Minute,
}

func Load() *Config {
	return &Config{
		Port:        envutil.String("PORT", "30190"),
		Environment: envutil.String("ENV", "production"),
		ForceHTTPS:  envutil.Bool("FORCE_HTTPS", true),
		Log: LogConfig{
			Level:      envutil.String("LOG_LEVEL", "info"),
			Dir:        envutil.String("LOG_DIR", "/app/logs"),
			MaxSizeMB:  envutil.Int("LOG_FILE_MAX_SIZE_MB", 1),
			MaxBackups: envutil.Int("LOG_FILE_MAX_BACKUPS", 30),
			MaxAgeDays: envutil.Int("LOG_FILE_MAX_AGE_DAYS", 7),
			Compress:   envutil.Bool("LOG_FILE_COMPRESS", true),
		},

		TLSEnabled:  envutil.Bool("TLS_ENABLED", false),
		TLSCertPath: envutil.String("TLS_CERT_PATH", "/certs/localhost.crt"),
		TLSKeyPath:  envutil.String("TLS_KEY_PATH", "/certs/localhost.key"),

		AdminUser:            envutil.String("ADMIN_USER", "admin"),
		AdminPassHash:        envutil.StringAny("ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT"),
		AdminSecretKey:       envutil.StringAny("SESSION_SECRET", "ADMIN_SECRET_KEY"),
		SessionTokenRotation: envutil.Bool("SESSION_TOKEN_ROTATION", true),

		ValkeyURL:  envutil.String("VALKEY_URL", "valkey-cache:6379"),
		DockerHost: envutil.String("DOCKER_HOST", "tcp://docker-proxy:2375"),

		HoloBotURL:    envutil.String("HOLO_BOT_URL", "http://hololive-kakao-bot-go:30001"),
		HoloBotAPIKey: envutil.String("HOLO_BOT_API_KEY", ""),

		OTELEnabled:     envutil.Bool("OTEL_ENABLED", false),
		OTELEndpoint:    envutil.String("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317"),
		OTELServiceName: envutil.String("OTEL_SERVICE_NAME", "admin-dashboard"),
		OTLPInsecure:    envutil.Bool("OTEL_EXPORTER_OTLP_INSECURE", true),
	}
}
