// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func LoadValkeyConfig() (ValkeyConfig, error) {
	var env envload.StrictEnv

	config := ValkeyConfig{
		Host:       sharedenv.String("CACHE_HOST", "localhost"),
		Port:       env.Int("CACHE_PORT", 6379),
		Password:   sharedenv.StringRaw("CACHE_PASSWORD", ""),
		DB:         env.Int("CACHE_DB", 0),
		SocketPath: sharedenv.String("CACHE_SOCKET_PATH", ""),
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

// LoadPostgresEndpointConfig는 plane별 pool 정책과 독립적인 연결 설정만 읽는다.
func LoadPostgresEndpointConfig() (PostgresConfig, error) {
	password := sharedenv.StringRaw("POSTGRES_PASSWORD", "")
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}

	var env envload.StrictEnv

	config := PostgresConfig{
		Host:          sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:          env.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:    sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:          sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:      password,
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

// LoadServerConfig는 API_SECRET_KEY를 인증 키로 쓰는 runtime의 HTTP listener 설정을 읽는다.
func LoadServerConfig() (ServerConfig, error) {
	return LoadServerConfigWithAPIKey(sharedenv.String("API_SECRET_KEY", ""))
}

// LoadServerConfigWithAPIKey: 런타임마다 인증 키 환경변수가 달라 호출자가 값을 넘긴다.
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

// LoadSettingsFilePath는 관리 화면이 저장하는 persisted settings(JSON) 경로다. SETTINGS_DIR(기본 data)/settings.json.
func LoadSettingsFilePath() string {
	dir := strings.TrimSpace(sharedenv.String("SETTINGS_DIR", ""))
	if dir == "" {
		dir = "data"
	}

	return filepath.Join(dir, "settings.json")
}
