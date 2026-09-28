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

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func LoadValkeyConfig() (ValkeyConfig, error) {
	var env load.StrictEnv

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
	password := sharedenv.StringRaw("POSTGRES_PASSWORD", "")
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}

	var env load.StrictEnv

	config := PostgresConfig{
		Host:          sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:          env.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:    sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:          sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:      password,
		Database:      sharedenv.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:       sharedenv.String("POSTGRES_SSLMODE", load.PostgresSSLModeVerifyFull),
		SSLRootCert:   sharedenv.String("POSTGRES_SSLROOTCERT", ""),
		QueryExecMode: sharedenv.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:  env.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:  env.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
	}

	if err := env.Err(); err != nil {
		return PostgresConfig{}, fmt.Errorf("load postgres config: %w", err)
	}

	return config, nil
}

func loadServerConfig() (ServerConfig, error) {
	return LoadServerConfigWithAPIKey(sharedenv.String("API_SECRET_KEY", ""))
}

// LoadServerConfigWithAPIKey: 런타임마다 인증 키 환경변수가 달라 호출자가 값을 넘긴다.
func LoadServerConfigWithAPIKey(apiKey string) (ServerConfig, error) {
	var env load.StrictEnv

	port := env.Int("SERVER_PORT", 30001)
	config := ServerConfig{
		AuthBcryptCost:          env.Int("AUTH_BCRYPT_COST", 0),
		Port:                    port,
		APIKey:                  apiKey,
		HTTPTransports:          load.CommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
		H3Addr:                  sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
		H3CertFile:              strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
		H3KeyFile:               strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
		ShortLinkAddr:           strings.TrimSpace(sharedenv.String("HOLOLIVE_SHORT_LINK_ADDR", "")),
		MetricsAddr:             strings.TrimSpace(sharedenv.String("HOLOLIVE_METRICS_ADDR", "")),
		PprofAddr:               strings.TrimSpace(sharedenv.String("HOLOLIVE_PPROF_ADDR", "")),
		AdminAllowedIPs:         load.CommaSeparated(sharedenv.String("ADMIN_ALLOWED_IPS", "")),
		WebSocketAllowedOrigins: load.CommaSeparated(sharedenv.String("WEBSOCKET_ALLOWED_ORIGINS", "")),
	}

	if err := env.Err(); err != nil {
		return ServerConfig{}, fmt.Errorf("load server config: %w", err)
	}

	return config, nil
}

func loadNotificationConfig() (NotificationConfig, error) {
	checkInterval, err := load.StrictDurationUnitEnv("CHECK_INTERVAL_SECONDS", time.Minute, time.Second)
	if err != nil {
		return NotificationConfig{}, fmt.Errorf("load notification config: %w", err)
	}

	return NotificationConfig{
		AdvanceMinutes:        parseIntList(sharedenv.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
		CheckInterval:         checkInterval,
		AlarmShortLinkBaseURL: strings.TrimSpace(sharedenv.String("ALARM_SHORT_LINK_BASE_URL", "")),
	}, nil
}

func LoadCliproxyConfig() (CliproxyConfig, error) {
	enabled, err := sharedenv.BoolE("CLIPROXY_ENABLED", false)
	if err != nil {
		return CliproxyConfig{}, fmt.Errorf("load cliproxy config: %w", err)
	}

	return CliproxyConfig{
		BaseURL:         sharedenv.String("CLIPROXY_BASE_URL", ""),
		APIKey:          sharedenv.String("CLIPROXY_API_KEY", ""),
		Model:           sharedenv.String("CLIPROXY_MODEL", "gpt-5.4"),
		Enabled:         enabled,
		ReasoningEffort: sharedenv.String("CLIPROXY_REASONING_EFFORT", "high"),
	}, nil
}

func LoadGeminiConfig() (GeminiConfig, error) {
	enabled, err := sharedenv.BoolE("GEMINI_ENABLED", false)
	if err != nil {
		return GeminiConfig{}, fmt.Errorf("load gemini config: %w", err)
	}

	return GeminiConfig{
		BaseURL:       sharedenv.String("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
		APIKey:        sharedenv.String("GEMINI_API_KEY", ""),
		Model:         sharedenv.String("GEMINI_MODEL", "gemini-3.7-flash"),
		Enabled:       enabled,
		ThinkingLevel: sharedenv.String("GEMINI_THINKING_LEVEL", "high"),
	}, nil
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
func loadConsensusLLMConfig(env *load.StrictEnv, prefix string) ConsensusLLMConfig {
	reviewTimeout := env.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}

	adjudicateTimeout := env.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           env.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(env.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     sharedenv.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  sharedenv.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func LoadLLMConfig() (LLMConfig, error) {
	var env load.StrictEnv

	config := LLMConfig{
		MemberNewsModel:       sharedenv.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: env.Float("MEMBER_NEWS_TEMPERATURE", 0),
		MemberNews:            loadConsensusLLMConfig(&env, "MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig(&env, "MAJOREVENT"),
	}

	if err := env.Err(); err != nil {
		return LLMConfig{}, fmt.Errorf("load LLM config: %w", err)
	}

	return config, nil
}

func LoadExaConfig() (ExaConfig, error) {
	enabled, err := sharedenv.BoolE("EXA_ENABLED", false)
	if err != nil {
		return ExaConfig{}, fmt.Errorf("load exa config: %w", err)
	}

	return ExaConfig{
		Endpoint: sharedenv.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   sharedenv.String("EXA_API_KEY", ""),
		Enabled:  enabled,
	}, nil
}

func loadSettingsFilePath() string {
	dir := strings.TrimSpace(sharedenv.String("SETTINGS_DIR", ""))
	if dir == "" {
		dir = "data"
	}

	return filepath.Join(dir, "settings.json")
}
