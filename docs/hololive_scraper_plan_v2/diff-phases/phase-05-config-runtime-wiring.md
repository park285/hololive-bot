# Phase 05. Config/env/runtime wiring

## 목표

Phase 01~04의 기능을 운영 config로 제어합니다.

추가할 config:

- channel health enable/TTL/base/max
- snapshot enable/dir/max bytes/min interval
- browser diagnostic config는 다음 phase에서 별도 연결

## 코드 레벨 의사결정

1. snapshot은 기본 OFF입니다.
2. channel health는 기본 ON입니다.
3. env 이름은 `SCRAPER_*` prefix로 통일합니다.
4. runtime wiring은 이미 scraper client를 만드는 `buildSharedYouTubeScraperClient`에 둡니다.

## 변경 대상

- `config/config_types.go`
- `config/config_env_loaders.go`
- `stream-ingester/internal/runtime/stream_ingester_youtube_components.go`

## Diff

```diff
diff --git a/hololive/hololive-shared/pkg/config/config_types.go b/hololive/hololive-shared/pkg/config/config_types.go
index 8f1baac..1115555 100644
--- a/hololive/hololive-shared/pkg/config/config_types.go
+++ b/hololive/hololive-shared/pkg/config/config_types.go
@@
 const (
 	ScraperFetcherEngineNetHTTP  = "nethttp"
 	ScraperFetcherEngineGoScrapy = "goscrapy"
+	ScraperFetcherEngineBrowserSnapshot = "browser_snapshot"
 )
@@
 func NormalizeScraperFetcherEngine(value string) string {
 	normalized := strings.ToLower(strings.TrimSpace(value))
-	if normalized == "" {
+	switch normalized {
+	case "", ScraperFetcherEngineNetHTTP:
 		return DefaultScraperFetcherEngine()
+	case ScraperFetcherEngineGoScrapy, ScraperFetcherEngineBrowserSnapshot:
+		return normalized
+	default:
+		return DefaultScraperFetcherEngine()
 	}
-	return normalized
 }
+
+type ScraperSnapshotConfig struct {
+	Enabled      bool
+	Dir          string
+	MaxBodyBytes int
+	MinInterval  time.Duration
+}
+
+type ScraperChannelHealthConfig struct {
+	Enabled           bool
+	TTL               time.Duration
+	ParserDriftBase   time.Duration
+	ParserDriftMax    time.Duration
+	TransportBase     time.Duration
+	TransportMax      time.Duration
+	TimeoutBase       time.Duration
+	TimeoutMax        time.Duration
+	HTTPStatusBase    time.Duration
+	HTTPStatusMax     time.Duration
+	SuccessDecaySteps int
+}
@@
 type ScraperConfig struct {
 	ProxyEnabled        bool
 	ProxyURL            string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
 	FetcherEngine       string
 	WorkerCount         int
 	Scheduler           ScraperSchedulerConfig
 	Poll                ScraperPoll
 	PublishedAtResolver ScraperPublishedAtResolverConfig
+	Snapshot            ScraperSnapshotConfig
+	ChannelHealth       ScraperChannelHealthConfig
 }
@@
 func DefaultScraperPublishedAtResolverConfig() ScraperPublishedAtResolverConfig {
@@
 }
+
+func DefaultScraperSnapshotConfig() ScraperSnapshotConfig {
+	return ScraperSnapshotConfig{
+		Enabled:      false,
+		Dir:          "./artifacts/youtube-scraper",
+		MaxBodyBytes: 512 << 10,
+		MinInterval:  30 * time.Minute,
+	}
+}
+
+func DefaultScraperChannelHealthConfig() ScraperChannelHealthConfig {
+	return ScraperChannelHealthConfig{
+		Enabled:           true,
+		TTL:               24 * time.Hour,
+		ParserDriftBase:   10 * time.Minute,
+		ParserDriftMax:    6 * time.Hour,
+		TransportBase:     2 * time.Minute,
+		TransportMax:      30 * time.Minute,
+		TimeoutBase:       2 * time.Minute,
+		TimeoutMax:        30 * time.Minute,
+		HTTPStatusBase:    5 * time.Minute,
+		HTTPStatusMax:     1 * time.Hour,
+		SuccessDecaySteps: 1,
+	}
+}
+
+func (c ScraperConfig) SnapshotOrDefault() ScraperSnapshotConfig {
+	defaults := DefaultScraperSnapshotConfig()
+	cfg := c.Snapshot
+	if strings.TrimSpace(cfg.Dir) == "" {
+		cfg.Dir = defaults.Dir
+	}
+	if cfg.MaxBodyBytes <= 0 {
+		cfg.MaxBodyBytes = defaults.MaxBodyBytes
+	}
+	if cfg.MinInterval <= 0 {
+		cfg.MinInterval = defaults.MinInterval
+	}
+	return cfg
+}
+
+func (c ScraperConfig) ChannelHealthOrDefault() ScraperChannelHealthConfig {
+	defaults := DefaultScraperChannelHealthConfig()
+	cfg := c.ChannelHealth
+	if cfg.TTL <= 0 {
+		cfg.TTL = defaults.TTL
+	}
+	if cfg.ParserDriftBase <= 0 {
+		cfg.ParserDriftBase = defaults.ParserDriftBase
+	}
+	if cfg.ParserDriftMax <= 0 {
+		cfg.ParserDriftMax = defaults.ParserDriftMax
+	}
+	if cfg.TransportBase <= 0 {
+		cfg.TransportBase = defaults.TransportBase
+	}
+	if cfg.TransportMax <= 0 {
+		cfg.TransportMax = defaults.TransportMax
+	}
+	if cfg.TimeoutBase <= 0 {
+		cfg.TimeoutBase = defaults.TimeoutBase
+	}
+	if cfg.TimeoutMax <= 0 {
+		cfg.TimeoutMax = defaults.TimeoutMax
+	}
+	if cfg.HTTPStatusBase <= 0 {
+		cfg.HTTPStatusBase = defaults.HTTPStatusBase
+	}
+	if cfg.HTTPStatusMax <= 0 {
+		cfg.HTTPStatusMax = defaults.HTTPStatusMax
+	}
+	if cfg.SuccessDecaySteps <= 0 {
+		cfg.SuccessDecaySteps = defaults.SuccessDecaySteps
+	}
+	return cfg
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/config/config_env_loaders.go b/hololive/hololive-shared/pkg/config/config_env_loaders.go
index 7e9cb38..2225555 100644
--- a/hololive/hololive-shared/pkg/config/config_env_loaders.go
+++ b/hololive/hololive-shared/pkg/config/config_env_loaders.go
@@
 func loadScraperConfig() ScraperConfig {
 	publishedAtResolverDefaults := DefaultScraperPublishedAtResolverConfig()
 	scraperSchedulerDefaults := DefaultScraperSchedulerConfig()
+	snapshotDefaults := DefaultScraperSnapshotConfig()
+	channelHealthDefaults := DefaultScraperChannelHealthConfig()

 	return ScraperConfig{
@@
 		PublishedAtResolver: ScraperPublishedAtResolverConfig{
@@
 			FailureBackoffTTL: time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS", int(publishedAtResolverDefaults.FailureBackoffTTL/time.Second))) * time.Second,
 		},
+		Snapshot: ScraperSnapshotConfig{
+			Enabled:      sharedenv.Bool("SCRAPER_SNAPSHOT_ENABLED", snapshotDefaults.Enabled),
+			Dir:          sharedenv.String("SCRAPER_SNAPSHOT_DIR", snapshotDefaults.Dir),
+			MaxBodyBytes: sharedenv.Int("SCRAPER_SNAPSHOT_MAX_BODY_BYTES", snapshotDefaults.MaxBodyBytes),
+			MinInterval:  time.Duration(sharedenv.Int("SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS", int(snapshotDefaults.MinInterval/time.Second))) * time.Second,
+		},
+		ChannelHealth: ScraperChannelHealthConfig{
+			Enabled:           sharedenv.Bool("SCRAPER_CHANNEL_HEALTH_ENABLED", channelHealthDefaults.Enabled),
+			TTL:               time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TTL_SECONDS", int(channelHealthDefaults.TTL/time.Second))) * time.Second,
+			ParserDriftBase:   time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_BASE_SECONDS", int(channelHealthDefaults.ParserDriftBase/time.Second))) * time.Second,
+			ParserDriftMax:    time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_MAX_SECONDS", int(channelHealthDefaults.ParserDriftMax/time.Second))) * time.Second,
+			TransportBase:     time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TRANSPORT_BASE_SECONDS", int(channelHealthDefaults.TransportBase/time.Second))) * time.Second,
+			TransportMax:      time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TRANSPORT_MAX_SECONDS", int(channelHealthDefaults.TransportMax/time.Second))) * time.Second,
+			TimeoutBase:       time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TIMEOUT_BASE_SECONDS", int(channelHealthDefaults.TimeoutBase/time.Second))) * time.Second,
+			TimeoutMax:        time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_TIMEOUT_MAX_SECONDS", int(channelHealthDefaults.TimeoutMax/time.Second))) * time.Second,
+			HTTPStatusBase:    time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_BASE_SECONDS", int(channelHealthDefaults.HTTPStatusBase/time.Second))) * time.Second,
+			HTTPStatusMax:     time.Duration(sharedenv.Int("SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_MAX_SECONDS", int(channelHealthDefaults.HTTPStatusMax/time.Second))) * time.Second,
+			SuccessDecaySteps: sharedenv.Int("SCRAPER_CHANNEL_HEALTH_SUCCESS_DECAY_STEPS", channelHealthDefaults.SuccessDecaySteps),
+		},
 	}
 }
```

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go
index 16e1a62..3335555 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go
@@
 func buildSharedYouTubeScraperClient(
 	scraperCfg config.ScraperConfig,
 	cacheService cache.Client,
 	sharedRL *scraper.RateLimiter,
 ) *scraper.Client {
@@
 		URL:     scraperCfg.ProxyURL,
 	}
+	snapshotCfg := scraperCfg.SnapshotOrDefault()
+	channelHealthCfg := scraperCfg.ChannelHealthOrDefault()

-	return scraper.NewClient(
+	opts := []scraper.ClientOption{
 		scraper.WithProxy(proxyConfig),
 		scraper.WithRateLimiter(sharedRL),
 		scraper.WithStateStore(cacheService),
 		scraper.WithFetcherEngine(scraper.FetcherEngine(scraperCfg.FetcherEngine)),
-	)
+	}
+
+	if channelHealthCfg.Enabled {
+		opts = append(opts, scraper.WithChannelHealthPolicy(scraper.ChannelHealthPolicy{
+			TTL:               channelHealthCfg.TTL,
+			ParserDriftBase:   channelHealthCfg.ParserDriftBase,
+			ParserDriftMax:    channelHealthCfg.ParserDriftMax,
+			TransportBase:     channelHealthCfg.TransportBase,
+			TransportMax:      channelHealthCfg.TransportMax,
+			TimeoutBase:       channelHealthCfg.TimeoutBase,
+			TimeoutMax:        channelHealthCfg.TimeoutMax,
+			HTTPStatusBase:    channelHealthCfg.HTTPStatusBase,
+			HTTPStatusMax:     channelHealthCfg.HTTPStatusMax,
+			SuccessDecaySteps: channelHealthCfg.SuccessDecaySteps,
+		}))
+	}
+
+	opts = append(opts, scraper.WithSnapshotPolicy(scraper.SnapshotPolicy{
+		Enabled:      snapshotCfg.Enabled,
+		MaxBodyBytes: snapshotCfg.MaxBodyBytes,
+		MinInterval:  snapshotCfg.MinInterval,
+		AllowedReasons: map[scraper.FailureReason]bool{
+			scraper.FailureReasonParserDrift:   true,
+			scraper.FailureReasonEmptyResponse: true,
+		},
+	}))
+	if snapshotCfg.Enabled {
+		opts = append(opts, scraper.WithSnapshotSink(scraper.NewFileSnapshotSink(snapshotCfg.Dir)))
+	}
+
+	return scraper.NewClient(opts...)
 }
```

## 실행

```bash
go test ./hololive/hololive-shared/pkg/config
go test ./hololive/hololive-stream-ingester/internal/runtime
```

## 완료 기준

- `SCRAPER_SNAPSHOT_ENABLED=false` 기본값에서는 snapshot 저장이 발생하지 않습니다.
- `SCRAPER_CHANNEL_HEALTH_ENABLED=true` 기본값에서 channel health가 동작합니다.
- config normalize가 알 수 없는 fetcher engine을 기본값으로 돌립니다.
- runtime에서 scraper client option wiring이 한 곳에 모입니다.
