package config

import (
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func loadValkeyConfig() ValkeyConfig {
	var raw valkeyEnvConfig
	_ = envconfig.Process("", &raw)

	return ValkeyConfig{
		Host:       parseStringWithDefault(raw.Host, "localhost"),
		Port:       parseIntWithDefault(raw.Port, 6379),
		Password:   raw.Password,
		DB:         parseIntWithDefault(raw.DB, 0),
		SocketPath: strings.TrimSpace(raw.SocketPath),
	}
}

func loadPostgresConfig() PostgresConfig {
	var raw postgresEnvConfig
	_ = envconfig.Process("", &raw)

	host := strings.TrimSpace(raw.Host)
	if host == "" {
		host = constants.DatabaseDefaults.Host
	}
	user := strings.TrimSpace(raw.User)
	if user == "" {
		user = constants.DatabaseDefaults.User
	}
	password := raw.Password
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}
	database := strings.TrimSpace(raw.Database)
	if database == "" {
		database = constants.DatabaseDefaults.Database
	}

	return PostgresConfig{
		Host:              host,
		Port:              parseIntWithDefault(raw.Port, constants.DatabaseDefaults.Port),
		SocketPath:        strings.TrimSpace(raw.SocketPath),
		User:              user,
		Password:          password,
		Database:          database,
		SSLMode:           parseStringWithDefault(raw.SSLMode, "require"),
		QueryExecMode:     parseStringWithDefault(raw.QueryExecMode, "cache_statement"),
		PoolMinConns:      parseIntWithDefault(raw.PoolMinConns, constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      parseIntWithDefault(raw.PoolMaxConns, constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  parseIntWithDefault(raw.PoolMaxIdleConns, constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: parseBoolWithDefault(raw.AutoPrepareSchema, true),
	}
}

func loadCliproxyConfig() CliproxyConfig {
	var raw cliproxyEnvConfig
	_ = envconfig.Process("", &raw)

	return CliproxyConfig{
		BaseURL:         parseStringWithDefault(raw.BaseURL, "https://cliproxy.capu.blog/v1"),
		APIKey:          strings.TrimSpace(raw.APIKey),
		Model:           parseStringWithDefault(raw.Model, "gpt-5.3-codex"),
		Enabled:         parseBoolWithDefault(raw.Enabled, false),
		ReasoningEffort: parseStringWithDefault(raw.ReasoningEffort, "high"),
	}
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
// prefix 예: "MEMBER_NEWS" -> MEMBER_NEWS_CONSENSUS_ENABLED, MEMBER_NEWS_CONSENSUS_CONFIDENCE, ...
func loadConsensusLLMConfig(prefix string) ConsensusLLMConfig {
	var raw consensusLLMEnvConfig
	_ = envconfig.Process(prefix, &raw)

	reviewTimeout := parseIntWithDefault(raw.ReviewTimeoutSec, 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := parseIntWithDefault(raw.AdjudicateTimeoutSec, 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           parseBoolWithDefault(raw.ConsensusEnabled, false),
		Confidence:        clampConfidence(parseFloatWithDefault(raw.ConsensusConfidence, 0.85)),
		ReviewerModel:     strings.TrimSpace(raw.ReviewerModel),
		AdjudicatorModel:  strings.TrimSpace(raw.AdjudicatorModel),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	var raw llmEnvConfig
	_ = envconfig.Process("", &raw)

	return LLMConfig{
		MemberNewsModel:       strings.TrimSpace(raw.MemberNewsModel),
		MemberNewsTemperature: parseFloatWithDefault(raw.MemberNewsTemperature, 0), // GPT-5: temperature=1.0만 지원, 0=미설정(SDK 기본값)
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	var raw exaEnvConfig
	_ = envconfig.Process("", &raw)

	return ExaConfig{
		Endpoint: parseStringWithDefault(raw.Endpoint, "https://mcp.exa.ai/mcp"),
		APIKey:   strings.TrimSpace(raw.APIKey),
		Enabled:  parseBoolWithDefault(raw.Enabled, false),
	}
}

func loadTelemetryConfig() TelemetryConfig {
	var raw telemetryEnvConfig
	_ = envconfig.Process("", &raw)

	metricsExportIntervalSeconds := parseIntWithDefault(raw.MetricsExportIntervalSec, 30)
	if metricsExportIntervalSeconds <= 0 {
		metricsExportIntervalSeconds = 30
	}

	return TelemetryConfig{
		Enabled:               parseBoolWithDefault(raw.Enabled, false),
		MetricsEnabled:        parseBoolWithDefault(raw.MetricsEnabled, false),
		MetricsExportInterval: time.Duration(metricsExportIntervalSeconds) * time.Second,
		ServiceName:           parseStringWithDefault(raw.ServiceName, "hololive-bot"),
		ServiceVersion:        parseStringWithDefault(raw.ServiceVersion, "1.0.0"),
		Environment:           parseStringWithDefault(raw.Environment, "production"),
		OTLPEndpoint:          parseStringWithDefault(raw.OTLPEndpoint, "otel-collector:4317"),
		OTLPInsecure:          parseBoolWithDefault(raw.OTLPInsecure, false),
		SampleRate:            parseFloatWithDefault(raw.SampleRate, 1.0),
	}
}
