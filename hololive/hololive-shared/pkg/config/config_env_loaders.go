package config

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/envutil"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func loadValkeyConfig() ValkeyConfig {
	return ValkeyConfig{
		Host:       envutil.String("CACHE_HOST", "localhost"),
		Port:       envutil.Int("CACHE_PORT", 6379),
		Password:   envutil.StringRaw("CACHE_PASSWORD", ""),
		DB:         envutil.Int("CACHE_DB", 0),
		SocketPath: envutil.String("CACHE_SOCKET_PATH", ""),
	}
}

func loadPostgresConfig() PostgresConfig {
	password := envutil.StringRaw("POSTGRES_PASSWORD", "")
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}

	return PostgresConfig{
		Host:              envutil.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:              envutil.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:        envutil.String("POSTGRES_SOCKET_PATH", ""),
		User:              envutil.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:          password,
		Database:          envutil.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:           envutil.String("POSTGRES_SSLMODE", "require"),
		QueryExecMode:     envutil.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:      envutil.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      envutil.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  envutil.Int("POSTGRES_POOL_MAX_IDLE_CONNS", constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: envutil.Bool("POSTGRES_AUTO_PREPARE_SCHEMA", true),
	}
}

func loadCliproxyConfig() CliproxyConfig {
	return CliproxyConfig{
		BaseURL:         envutil.String("CLIPROXY_BASE_URL", "https://cliproxy.capu.blog/v1"),
		APIKey:          envutil.String("CLIPROXY_API_KEY", ""),
		Model:           envutil.String("CLIPROXY_MODEL", "gpt-5.3-codex"),
		Enabled:         envutil.Bool("CLIPROXY_ENABLED", false),
		ReasoningEffort: envutil.String("CLIPROXY_REASONING_EFFORT", "high"),
	}
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
// prefix 예: "MEMBER_NEWS" -> MEMBER_NEWS_CONSENSUS_ENABLED, MEMBER_NEWS_CONSENSUS_CONFIDENCE, ...
func loadConsensusLLMConfig(prefix string) ConsensusLLMConfig {
	reviewTimeout := envutil.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := envutil.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           envutil.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(envutil.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     envutil.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  envutil.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	return LLMConfig{
		MemberNewsModel:       envutil.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: envutil.Float("MEMBER_NEWS_TEMPERATURE", 0),
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	return ExaConfig{
		Endpoint: envutil.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   envutil.String("EXA_API_KEY", ""),
		Enabled:  envutil.Bool("EXA_ENABLED", false),
	}
}

func loadTelemetryConfig() TelemetryConfig {
	metricsExportIntervalSeconds := envutil.Int("OTEL_METRICS_EXPORT_INTERVAL_SECONDS", 30)
	if metricsExportIntervalSeconds <= 0 {
		metricsExportIntervalSeconds = 30
	}

	return TelemetryConfig{
		Enabled:               envutil.Bool("OTEL_ENABLED", false),
		MetricsEnabled:        envutil.Bool("OTEL_METRICS_ENABLED", false),
		MetricsExportInterval: time.Duration(metricsExportIntervalSeconds) * time.Second,
		ServiceName:           envutil.String("OTEL_SERVICE_NAME", "hololive-bot"),
		ServiceVersion:        envutil.String("OTEL_SERVICE_VERSION", "1.0.0"),
		Environment:           envutil.String("OTEL_ENVIRONMENT", "production"),
		OTLPEndpoint:          envutil.String("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317"),
		OTLPInsecure:          envutil.Bool("OTEL_EXPORTER_OTLP_INSECURE", false),
		SampleRate:            envutil.Float("OTEL_SAMPLE_RATE", 1.0),
	}
}
