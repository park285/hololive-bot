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
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/workerconfig"

	"github.com/kapu/hololive-shared/pkg/constants"
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
		Host:          sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:          sharedenv.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:    sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:          sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:      password,
		Database:      sharedenv.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:       sharedenv.String("POSTGRES_SSLMODE", "verify-full"),
		SSLRootCert:   sharedenv.String("POSTGRES_SSLROOTCERT", ""),
		QueryExecMode: sharedenv.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:  sharedenv.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:  sharedenv.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
	}
}

func loadServerConfig() ServerConfig {
	port := sharedenv.Int("SERVER_PORT", 30001)

	return ServerConfig{
		Port:            port,
		APIKey:          sharedenv.String("API_SECRET_KEY", ""),
		HTTPTransports:  parseCommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
		H3Addr:          sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
		H3CertFile:      strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
		H3KeyFile:       strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
		MetricsAddr:     strings.TrimSpace(sharedenv.String("HOLOLIVE_METRICS_ADDR", "")),
		PprofAddr:       strings.TrimSpace(sharedenv.String("HOLOLIVE_PPROF_ADDR", "")),
		AdminAllowedIPs: parseCommaSeparated(sharedenv.String("ADMIN_ALLOWED_IPS", "")),
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
		Backfill:          loadScraperBackfillConfig(),
		ActiveActive:      loadScraperActiveActiveConfig(),
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

func loadScraperBackfillConfig() ScraperBackfillConfig {
	defaults := DefaultScraperBackfillConfig()
	return ScraperBackfillConfig{
		Enabled:           sharedenv.Bool("SCRAPER_BACKFILL_ENABLED", defaults.Enabled),
		ShortsEnabled:     sharedenv.Bool("SCRAPER_BACKFILL_SHORTS_ENABLED", defaults.ShortsEnabled),
		ShortsInterval:    time.Duration(sharedenv.Int("SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS", int(defaults.ShortsInterval/time.Second))) * time.Second,
		CommunityEnabled:  sharedenv.Bool("SCRAPER_BACKFILL_COMMUNITY_ENABLED", defaults.CommunityEnabled),
		CommunityInterval: time.Duration(sharedenv.Int("SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS", int(defaults.CommunityInterval/time.Second))) * time.Second,
		LiveEnabled:       sharedenv.Bool("SCRAPER_BACKFILL_LIVE_ENABLED", defaults.LiveEnabled),
		LiveInterval:      time.Duration(sharedenv.Int("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS", int(defaults.LiveInterval/time.Second))) * time.Second,
		TargetGroup:       strings.TrimSpace(sharedenv.String("SCRAPER_BACKFILL_TARGET_GROUP", defaults.TargetGroup)),
	}
}

func loadScraperActiveActiveConfig() ScraperActiveActiveConfig {
	defaults := DefaultScraperActiveActiveConfig()
	return ScraperActiveActiveConfig{
		Enabled:    sharedenv.Bool("YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED", defaults.Enabled),
		InstanceID: strings.TrimSpace(sharedenv.String("YOUTUBE_PRODUCER_INSTANCE_ID", "")),
		Namespace:  strings.TrimSpace(sharedenv.String("YOUTUBE_PRODUCER_LEASE_NAMESPACE", defaults.Namespace)),
	}
}

func LoadYouTubeProducerGlobalBudgetConfig() YouTubeProducerGlobalBudgetConfig {
	defaults := DefaultYouTubeProducerGlobalBudgetConfig()
	return YouTubeProducerGlobalBudgetConfig{
		Enabled:                    sharedenv.Bool("YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED", defaults.Enabled),
		AcquireTimeout:             loadYouTubeProducerBudgetAcquireTimeout(defaults.AcquireTimeout),
		ActiveInstanceCount:        sharedenv.Int("YOUTUBE_PRODUCER_ACTIVE_ACTIVE_INSTANCE_COUNT", defaults.ActiveInstanceCount),
		YouTubeScraperMaxInflight:  loadYouTubeProducerBudgetMaxInflight("YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT", defaults.YouTubeScraperMaxInflight),
		HolodexLiveMaxInflight:     loadYouTubeProducerBudgetMaxInflight("YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT", defaults.HolodexLiveMaxInflight),
		BrowserSnapshotMaxInflight: loadYouTubeProducerBudgetMaxInflight("YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT", defaults.BrowserSnapshotMaxInflight),
		BackfillMaxInflight:        loadYouTubeProducerBudgetMaxInflight("YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT", defaults.BackfillMaxInflight),
		FallbackMaxInflight:        loadYouTubeProducerBudgetMaxInflight("YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT", defaults.FallbackMaxInflight),
		CleanupLimit:               loadYouTubeProducerBudgetCleanupLimit(defaults.CleanupLimit),
		WindowCheckEnabled:         sharedenv.Bool("YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED", defaults.WindowCheckEnabled),
	}
}

func loadYouTubeProducerBudgetAcquireTimeout(defaultTimeout time.Duration) time.Duration {
	timeout := time.Duration(sharedenv.Int("YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS", int(defaultTimeout/time.Millisecond))) * time.Millisecond
	if timeout <= 0 {
		return defaultTimeout
	}
	if timeout > 5*time.Second {
		return 5 * time.Second
	}
	return timeout
}

func loadYouTubeProducerBudgetMaxInflight(key string, defaultValue int) int {
	value := sharedenv.Int(key, defaultValue)
	if value < 0 {
		return 0
	}
	return value
}

func loadYouTubeProducerBudgetCleanupLimit(defaultValue int) int {
	value := sharedenv.Int("YOUTUBE_PRODUCER_BUDGET_CLEANUP_LIMIT", defaultValue)
	if value <= 0 {
		return defaultValue
	}
	return value
}

func loadWorkerPoolConfig(profile workerconfig.IrisBotWebhookWorkerProfile) WorkerPoolConfig {
	return WorkerPoolConfig{
		Workers:   profile.BotPool.Workers,
		QueueSize: profile.BotPool.QueueSize,
	}
}

func loadWebhookConfig(profile workerconfig.IrisBotWebhookWorkerProfile) WebhookConfig {
	return WebhookConfig{
		WorkerCount:    profile.Receive.Workers,
		QueueSize:      profile.Receive.QueueSize,
		EnqueueTimeout: profile.Receive.EnqueueTimeout,
		HandlerTimeout: profile.Receive.HandlerTimeout,
		MaxBodyBytes:   profile.Receive.MaxBodyBytes,
		DedupTTL:       profile.Receive.DedupTTL,
		DedupTimeout:   profile.Receive.DedupTimeout,
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
		MonthlyTokenCeiling:   int64(sharedenv.Int("LLM_MONTHLY_TOKEN_CEILING", 0)),
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
