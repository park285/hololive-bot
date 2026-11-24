package settings

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func LoadValkeyConfig() (ValkeyConfig, error) {
	var env envload.StrictEnv

	password, err := sharedenv.StringOrSecretFile("CACHE_PASSWORD")
	if err != nil {
		return ValkeyConfig{}, fmt.Errorf("load CACHE_PASSWORD: %w", err)
	}

	config := ValkeyConfig{
		Host:       sharedenv.String("CACHE_HOST", "localhost"),
		Port:       env.Int("CACHE_PORT", 6379),
		Username:   sharedenv.String("CACHE_USERNAME", ""),
		Password:   password,
		DB:         env.Int("CACHE_DB", 0),
		SocketPath: sharedenv.String("CACHE_SOCKET_PATH", ""),
		TLSCAFile:  sharedenv.String("CACHE_TLS_CA_FILE", ""),
	}

	if err := env.Err(); err != nil {
		return ValkeyConfig{}, fmt.Errorf("load valkey config: %w", err)
	}

	return config, nil
}

func LoadPostgresConfig() (PostgresConfig, error) {
	config, err := LoadPostgresEndpointConfig()
	if err != nil {
		return PostgresConfig{}, err
	}

	var env envload.StrictEnv

	config.PoolMinConns = env.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns)
	config.PoolMaxConns = env.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns)

	if err := env.Err(); err != nil {
		return PostgresConfig{}, fmt.Errorf("load postgres pool config: %w", err)
	}

	return config, nil
}

func LoadPostgresEndpointConfig() (PostgresConfig, error) {
	var env envload.StrictEnv

	config := PostgresConfig{
		Host:          sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:          env.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:    sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:          sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:      sharedenv.StringRaw("POSTGRES_PASSWORD", ""),
		Database:      sharedenv.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:       sharedenv.String("POSTGRES_SSLMODE", runtimepolicy.PostgresSSLModeVerifyFull),
		SSLRootCert:   sharedenv.String("POSTGRES_SSLROOTCERT", ""),
		QueryExecMode: sharedenv.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
	}

	if err := env.Err(); err != nil {
		return PostgresConfig{}, fmt.Errorf("load postgres config: %w", err)
	}

	return config, nil
}

func LoadServerConfig() (ServerConfig, error) {
	return LoadServerConfigWithAPIKey(sharedenv.String("API_SECRET_KEY", ""))
}

func LoadServerConfigWithAPIKey(apiKey string) (ServerConfig, error) {
	var env envload.StrictEnv

	port := env.Int("SERVER_PORT", 30001)
	config := ServerConfig{
		Port:           port,
		APIKey:         apiKey,
		HTTPTransports: envload.CommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
		H3Addr:         sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
		H3CertFile:     strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
		H3KeyFile:      strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
		ShortLinkAddr:  strings.TrimSpace(sharedenv.String("HOLOLIVE_SHORT_LINK_ADDR", "")),
		MetricsAddr:    strings.TrimSpace(sharedenv.String("HOLOLIVE_METRICS_ADDR", "")),
		PprofAddr:      strings.TrimSpace(sharedenv.String("HOLOLIVE_PPROF_ADDR", "")),
	}

	if err := env.Err(); err != nil {
		return ServerConfig{}, fmt.Errorf("load server config: %w", err)
	}

	return config, nil
}

func LoadNotificationConfig() (NotificationConfig, error) {
	checkInterval, err := envload.StrictDurationUnitEnv("CHECK_INTERVAL_SECONDS", time.Minute, time.Second)
	if err != nil {
		return NotificationConfig{}, fmt.Errorf("load notification config: %w", err)
	}

	return NotificationConfig{
		AdvanceMinutes:        parseIntList(sharedenv.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
		CheckInterval:         checkInterval,
		AlarmShortLinkBaseURL: strings.TrimSpace(sharedenv.String("ALARM_SHORT_LINK_BASE_URL", "")),
	}, nil
}

func LoadSettingsFilePath() string {
	dir := strings.TrimSpace(sharedenv.String("SETTINGS_DIR", ""))
	if dir == "" {
		dir = "data"
	}

	return filepath.Join(dir, "settings.json")
}
