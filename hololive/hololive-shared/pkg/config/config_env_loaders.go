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

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedenv "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
)

func loadAppEnvironment() string {
	return sharedenv.String("APP_ENV", "production")
}

func loadValkeyConfig() ValkeyConfig {
	return ValkeyConfig{
		Host:       sharedenv.String("CACHE_HOST", "localhost"),
		Port:       sharedenv.Int("CACHE_PORT", 6379),
		Password:   sharedenv.StringRaw("CACHE_PASSWORD", ""),
		DB:         sharedenv.Int("CACHE_DB", 0),
		SocketPath: sharedenv.String("CACHE_SOCKET_PATH", ""),
	}
}

func loadPostgresConfig() PostgresConfig {
	password := sharedenv.StringRaw("POSTGRES_PASSWORD", "")
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}

	return PostgresConfig{
		Host:              sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:              sharedenv.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:        sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:              sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:          password,
		Database:          sharedenv.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:           sharedenv.String("POSTGRES_SSLMODE", "require"),
		QueryExecMode:     sharedenv.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:      sharedenv.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      sharedenv.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  sharedenv.Int("POSTGRES_POOL_MAX_IDLE_CONNS", constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: sharedenv.Bool("POSTGRES_AUTO_PREPARE_SCHEMA", true),
	}
}

func loadServerConfig() ServerConfig {
	port := sharedenv.Int("SERVER_PORT", 30001)

	return ServerConfig{
		Port:           port,
		APIKey:         sharedenv.String("API_SECRET_KEY", ""),
		HTTPTransports: parseCommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
		H2CAddr:        sharedenv.String("HOLOLIVE_H2C_ADDR", fmt.Sprintf(":%d", port)),
		H3Addr:         sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
		H3CertFile:     strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
		H3KeyFile:      strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
	}
}

func loadNotificationConfig() NotificationConfig {
	return NotificationConfig{
		AdvanceMinutes: parseIntList(sharedenv.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
		CheckInterval:  time.Duration(sharedenv.Int("CHECK_INTERVAL_SECONDS", 60)) * time.Second,
	}
}

func loadScraperConfig() ScraperConfig {
	publishedAtResolverDefaults := DefaultScraperPublishedAtResolverConfig()
	scraperSchedulerDefaults := DefaultScraperSchedulerConfig()
	snapshotDefaults := DefaultScraperSnapshotConfig()

	return ScraperConfig{
		ProxyEnabled:  sharedenv.Bool("SCRAPER_PROXY_ENABLED", false),
		ProxyURL:      sharedenv.String("SCRAPER_PROXY_URL", ""),
		FetcherEngine: NormalizeScraperFetcherEngine(sharedenv.String("SCRAPER_FETCHER_ENGINE", DefaultScraperFetcherEngine())),
		WorkerCount: intAliasEnv([]string{
			"SCRAPER_SCHEDULER_WORKER_COUNT",
			"SCRAPER_WORKER_COUNT",
		}, DefaultScraperWorkerCount()),
		Scheduler: ScraperSchedulerConfig{
			PollTimeout:     time.Duration(sharedenv.Int("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS", int(scraperSchedulerDefaults.PollTimeout/time.Second))) * time.Second,
			ErrorBackoffMin: time.Duration(sharedenv.Int("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS", int(scraperSchedulerDefaults.ErrorBackoffMin/time.Second))) * time.Second,
			ErrorBackoffMax: time.Duration(sharedenv.Int("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS", int(scraperSchedulerDefaults.ErrorBackoffMax/time.Second))) * time.Second,
		},
		Poll: loadScraperPoll(),
		PublishedAtResolver: ScraperPublishedAtResolverConfig{
			Enabled:           sharedenv.Bool("SCRAPER_PUBLISHED_AT_RESOLVER_ENABLED", publishedAtResolverDefaults.Enabled),
			Interval:          time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS", int(publishedAtResolverDefaults.Interval/time.Second))) * time.Second,
			BatchSize:         sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE", publishedAtResolverDefaults.BatchSize),
			MaxResolvePerRun:  sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN", publishedAtResolverDefaults.MaxResolvePerRun),
			MaxRunDuration:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS", int(publishedAtResolverDefaults.MaxRunDuration/time.Second))) * time.Second,
			ResolveTimeout:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS", int(publishedAtResolverDefaults.ResolveTimeout/time.Second))) * time.Second,
			MinDetectedAge:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS", int(publishedAtResolverDefaults.MinDetectedAge/time.Second))) * time.Second,
			FailureBackoffTTL: time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS", int(publishedAtResolverDefaults.FailureBackoffTTL/time.Second))) * time.Second,
		},
		Snapshot: ScraperSnapshotConfig{
			Enabled:      sharedenv.Bool("SCRAPER_SNAPSHOT_ENABLED", snapshotDefaults.Enabled),
			Dir:          sharedenv.String("SCRAPER_SNAPSHOT_DIR", snapshotDefaults.Dir),
			MaxBodyBytes: sharedenv.Int("SCRAPER_SNAPSHOT_MAX_BODY_BYTES", snapshotDefaults.MaxBodyBytes),
			MinInterval:  time.Duration(sharedenv.Int("SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS", int(snapshotDefaults.MinInterval/time.Second))) * time.Second,
		},
		ChannelHealth:     loadScraperChannelHealthConfig(),
		BrowserDiagnostic: loadScraperBrowserDiagnosticConfig(),
		PollTiering:       loadScraperPollTieringConfig(),
	}
}

func loadScraperChannelHealthConfig() ScraperChannelHealthConfig {
	defaults := DefaultScraperChannelHealthConfig()
	return ScraperChannelHealthConfig{
		Enabled:           sharedenv.Bool("SCRAPER_CHANNEL_HEALTH_ENABLED", defaults.Enabled),
		Enforce:           sharedenv.Bool("SCRAPER_CHANNEL_HEALTH_ENFORCE", defaults.Enforce),
		TTL:               time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TTL_SECONDS", int(defaults.TTL/time.Second))) * time.Second,
		ParserDriftBase:   time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_BASE_SECONDS", int(defaults.ParserDriftBase/time.Second))) * time.Second,
		ParserDriftMax:    time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_MAX_SECONDS", int(defaults.ParserDriftMax/time.Second))) * time.Second,
		TransportBase:     time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TRANSPORT_BASE_SECONDS", int(defaults.TransportBase/time.Second))) * time.Second,
		TransportMax:      time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TRANSPORT_MAX_SECONDS", int(defaults.TransportMax/time.Second))) * time.Second,
		TimeoutBase:       time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TIMEOUT_BASE_SECONDS", int(defaults.TimeoutBase/time.Second))) * time.Second,
		TimeoutMax:        time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TIMEOUT_MAX_SECONDS", int(defaults.TimeoutMax/time.Second))) * time.Second,
		HTTPStatusBase:    time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_BASE_SECONDS", int(defaults.HTTPStatusBase/time.Second))) * time.Second,
		HTTPStatusMax:     time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_MAX_SECONDS", int(defaults.HTTPStatusMax/time.Second))) * time.Second,
		SuccessDecaySteps: sharedenv.Int("SCRAPER_CHANNEL_HEALTH_SUCCESS_DECAY_STEPS", defaults.SuccessDecaySteps),
	}
}

func loadScraperBrowserDiagnosticConfig() ScraperBrowserDiagnosticConfig {
	defaults := DefaultScraperBrowserDiagnosticConfig()
	return ScraperBrowserDiagnosticConfig{
		Enabled:  sharedenv.Bool("SCRAPER_BROWSER_DIAGNOSTIC_ENABLED", defaults.Enabled),
		Endpoint: sharedenv.String("SCRAPER_BROWSER_DIAGNOSTIC_ENDPOINT", defaults.Endpoint),
		Timeout:  time.Duration(sharedenv.Int("SCRAPER_BROWSER_DIAGNOSTIC_TIMEOUT_SECONDS", int(defaults.Timeout/time.Second))) * time.Second,
	}
}

func loadScraperPollTieringConfig() ScraperPollTieringConfig {
	defaults := DefaultScraperPollTieringConfig()
	return ScraperPollTieringConfig{
		Enabled: sharedenv.Bool("SCRAPER_POLL_TIERING_ENABLED", defaults.Enabled),
	}
}

func loadWebhookConfig() WebhookConfig {
	return WebhookConfig{
		WorkerCount:    sharedenv.Int("WEBHOOK_WORKER_COUNT", 16),
		QueueSize:      sharedenv.Int("WEBHOOK_QUEUE_SIZE", 1000),
		EnqueueTimeout: time.Duration(sharedenv.Int("WEBHOOK_ENQUEUE_TIMEOUT_MS", 50)) * time.Millisecond,
		HandlerTimeout: time.Duration(sharedenv.Int("WEBHOOK_HANDLER_TIMEOUT_SECONDS", 30)) * time.Second,
		RequireHTTP2:   sharedenv.Bool("WEBHOOK_REQUIRE_HTTP2", false),
	}
}

func loadCommunityShortsBigBangCutoverAt() (time.Time, error) {
	raw := strings.TrimSpace(sharedenv.String("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", ""))
	if raw == "" {
		return time.Time{}, nil
	}

	cutoverAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT must be RFC3339: %w", err)
	}

	return cutoverAt.UTC(), nil
}

func loadCliproxyConfig() CliproxyConfig {
	return CliproxyConfig{
		BaseURL:         sharedenv.String("CLIPROXY_BASE_URL", "https://cliproxy.capu.blog/v1"),
		APIKey:          sharedenv.String("CLIPROXY_API_KEY", ""),
		Model:           sharedenv.String("CLIPROXY_MODEL", "gpt-5.4"),
		Enabled:         sharedenv.Bool("CLIPROXY_ENABLED", false),
		ReasoningEffort: sharedenv.String("CLIPROXY_REASONING_EFFORT", "high"),
	}
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
// prefix 예: "MEMBER_NEWS" -> MEMBER_NEWS_CONSENSUS_ENABLED, MEMBER_NEWS_CONSENSUS_CONFIDENCE, ...
func loadConsensusLLMConfig(prefix string) ConsensusLLMConfig {
	reviewTimeout := sharedenv.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := sharedenv.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           sharedenv.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(sharedenv.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     sharedenv.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  sharedenv.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	return LLMConfig{
		MemberNewsModel:       sharedenv.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: sharedenv.Float("MEMBER_NEWS_TEMPERATURE", 0),
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	return ExaConfig{
		Endpoint: sharedenv.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   sharedenv.String("EXA_API_KEY", ""),
		Enabled:  sharedenv.Bool("EXA_ENABLED", false),
	}
}
