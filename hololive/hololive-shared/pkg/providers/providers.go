package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httpclient"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/llm"
	"github.com/kapu/hololive-shared/pkg/platform/bootstrap"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// ProvideValkeyConfig - 설정에서 Valkey 캐시 설정 추출
func ProvideValkeyConfig(cfg *config.Config) config.ValkeyConfig {
	return cfg.Valkey
}

// ProvidePostgresConfig - 설정에서 PostgreSQL 설정 추출
func ProvidePostgresConfig(cfg *config.Config) config.PostgresConfig {
	return cfg.Postgres
}

// ProvideCacheResources - 캐시 리소스 생성 (정리 함수 포함)
func ProvideCacheResources(ctx context.Context, cfg config.ValkeyConfig, logger *slog.Logger) (*bootstrap.CacheResources, func(), error) {
	resources, err := bootstrap.NewCacheResources(ctx, cfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache resources: %w", err)
	}
	return resources, resources.Close, nil
}

// ProvideCacheService - 캐시 리소스에서 서비스 추출
func ProvideCacheService(resources *bootstrap.CacheResources) *cache.Service {
	return resources.Service
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함)
func ProvideDatabaseResources(ctx context.Context, cfg config.PostgresConfig, logger *slog.Logger) (*bootstrap.DatabaseResources, func(), error) {
	resources, err := bootstrap.NewDatabaseResources(ctx, cfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}
	return resources, resources.Close, nil
}

// ProvidePostgresService - 데이터베이스 리소스에서 서비스 추출
func ProvidePostgresService(resources *bootstrap.DatabaseResources) *database.PostgresService {
	return resources.Service
}

// ProvideIrisClient - Iris h2c(HTTP/2 Cleartext) 클라이언트 생성
func ProvideIrisClient(cfg config.IrisConfig, logger *slog.Logger) iris.Client {
	return iris.NewH2CClient(cfg.BaseURL, cfg.BotToken, logger, iris.H2CClientOptions{
		Timeout:               cfg.HTTPTimeout,
		DialTimeout:           cfg.HTTPDialTimeout,
		ResponseHeaderTimeout: cfg.HTTPResponseHeaderTimeout,
	})
}

// ProvideMemberRepository - 멤버 저장소 생성
func ProvideMemberRepository(postgres *database.PostgresService, logger *slog.Logger) *member.Repository {
	return member.NewMemberRepository(postgres, logger)
}

// ProvideMemberCache - 멤버 캐시 생성 (초기 워밍업 포함)
func ProvideMemberCache(
	ctx context.Context,
	repo *member.Repository,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := buildMemberCache(ctx, repo, cacheSvc, logger)
	if err != nil {
		return nil, err
	}

	if cacheSvc == nil {
		logger.Warn("Cache service is nil; member database init skipped")
		return memberCache, nil
	}

	// Valkey member database 초기화 (이름 -> 채널ID 맵)
	members, err := repo.GetAllMembers(ctx)
	if err != nil {
		logger.Warn("Failed to load members for member database init", slog.Any("error", err))
		members = []*domain.Member{}
	}

	memberMap := make(map[string]string, len(members))
	for _, m := range members {
		if m != nil && m.ChannelID != "" {
			// name:org 형식으로 캐시 키 생성 (동명이인 지원)
			memberMap[m.Name+":"+m.GetOrg()] = m.ChannelID
		}
	}

	if err := cacheSvc.InitializeMemberDatabase(ctx, memberMap); err != nil {
		return nil, fmt.Errorf("failed to initialize member database: %w", err)
	}

	return memberCache, nil
}

// ProvideMemberCacheWithoutValkey - Valkey 없이 멤버 캐시만 구성
func ProvideMemberCacheWithoutValkey(
	ctx context.Context,
	repo *member.Repository,
	logger *slog.Logger,
) (*member.Cache, error) {
	return buildMemberCache(ctx, repo, nil, logger)
}

// ProvideMemberServiceAdapter - 멤버 데이터 제공자 어댑터 생성
func ProvideMemberServiceAdapter(memberCache *member.Cache, logger *slog.Logger) *member.ServiceAdapter {
	return member.NewMemberServiceAdapter(memberCache, logger)
}

// ProvideMembersData - 도메인 인터페이스로 바인딩
func ProvideMembersData(adapterSvc *member.ServiceAdapter) domain.MemberDataProvider {
	return adapterSvc
}

// ProvideHolodexAPIKeys - 설정에서 API 키 추출
func ProvideHolodexAPIKeys(cfg config.HolodexConfig) []string {
	return cfg.APIKeys
}

// ProvideScraperService - 스크래퍼 서비스 생성
func ProvideScraperService(
	cacheSvc *cache.Service,
	members *member.ServiceAdapter,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *holodex.ScraperService {
	return holodex.NewScraperService(cacheSvc, members, proxyConfig, sharedRL, logger)
}

// ProvideHolodexService - Holodex API 서비스 생성
func ProvideHolodexService(
	baseURL string,
	apiKeys []string,
	cacheSvc *cache.Service,
	scraperSvc *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	svc, err := holodex.NewHolodexService(baseURL, apiKeys, cacheSvc, scraperSvc, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}
	return svc, nil
}

// ProvideProfileService - 프로필 서비스 생성 (번역 사전 로드 포함)
func ProvideProfileService(
	ctx context.Context,
	cacheSvc *cache.Service,
	members *member.ServiceAdapter,
	logger *slog.Logger,
) (*member.ProfileService, error) {
	svc, err := member.NewProfileService(cacheSvc, members, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile service: %w", err)
	}
	svc.PreloadTranslations(ctx)
	return svc, nil
}

// ProvideMemberMatcher - 멤버 매칭 서비스 생성
func ProvideMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	logger *slog.Logger,
) *matcher.MemberMatcher {
	// selector는 nil (Gemini AI 채널 선택 미사용)
	return matcher.NewMemberMatcher(ctx, membersData, cacheSvc, holodexSvc, nil, logger)
}

// ProvideAlarmRepository - 알람 저장소 생성 (DB 영속화)
func ProvideAlarmRepository(
	postgres *database.PostgresService,
	logger *slog.Logger,
) *alarm.Repository {
	return alarm.NewRepository(postgres, logger)
}

// ProvideChzzkClient - Chzzk API 클라이언트 생성
func ProvideChzzkClient(httpClient *http.Client, cfg config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Logger:       logger,
	})
}

// ProvideTwitchClient - Twitch Helix API 클라이언트 생성
func ProvideTwitchClient(cfg config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	return twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, logger)
}

// ProvideAlarmService - 알림 서비스 생성
func ProvideAlarmService(
	advanceMinutes []int,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepo *alarm.Repository,
	logger *slog.Logger,
) (*notification.AlarmService, error) {
	svc, err := notification.NewAlarmService(
		cacheSvc,
		holodexSvc,
		chzzkClient,
		twitchClient,
		memberData,
		alarmRepo,
		logger,
		advanceMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm service: %w", err)
	}
	return svc, nil
}

// ProvideMajorEventLLMClient - MajorEvent 전용 LLM 클라이언트 생성 (비활성 시 nil)
func ProvideMajorEventLLMClient(cliproxy config.CliproxyConfig, logger *slog.Logger) llm.Client {
	if !cliproxy.Enabled || cliproxy.APIKey == "" {
		logger.Info("Cliproxy LLM disabled; event summaries will use template fallback")
		return nil
	}
	if cliproxy.BaseURL == "" || cliproxy.Model == "" {
		logger.Error("Cliproxy LLM configuration incomplete",
			slog.Bool("baseURL_set", cliproxy.BaseURL != ""),
			slog.Bool("model_set", cliproxy.Model != ""),
		)
		return nil
	}
	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, cliproxy.Model, logger,
		llm.WithWebSearch(true),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	)
	logger.Info("Cliproxy LLM enabled for event summaries (responses + web_search, chat fallback)",
		slog.String("model", cliproxy.Model),
		slog.String("reasoning_effort", cliproxy.ReasoningEffort))
	return client
}

// ProvideMemberNewsLLMClient: member news 전용 LLM 클라이언트 (schema name + temperature 오버라이드)
func ProvideMemberNewsLLMClient(cliproxy config.CliproxyConfig, llmCfg config.LLMConfig, logger *slog.Logger) llm.Client {
	if !cliproxy.Enabled || cliproxy.APIKey == "" {
		logger.Info("Member news LLM disabled")
		return nil
	}

	model := llmCfg.MemberNewsModel
	if model == "" {
		model = cliproxy.Model
	}

	if cliproxy.BaseURL == "" || model == "" {
		logger.Error("Member news LLM configuration incomplete",
			slog.Bool("baseURL_set", cliproxy.BaseURL != ""),
			slog.Bool("model_set", model != ""),
		)
		return nil
	}

	opts := []llm.Option{
		llm.WithSchemaName("member_news_summary"),
		llm.WithWebSearch(false), // 수집 완료된 데이터 요약이므로 web search 불필요
		llm.WithChatCompletions(),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	}
	if llmCfg.MemberNewsTemperature > 0 {
		opts = append(opts, llm.WithTemperature(llmCfg.MemberNewsTemperature))
	}

	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger, opts...)
	tempApplied := llmCfg.MemberNewsTemperature > 0
	logger.Info("Member news LLM enabled",
		slog.String("model", model),
		slog.Bool("temperature_applied", tempApplied),
		slog.Float64("temperature", llmCfg.MemberNewsTemperature),
	)
	return client
}

// ProvideMemberNewsReviewerClient: consensus reviewer 전용 LLM 클라이언트.
// consensus 비활성 또는 Cliproxy 비활성 시 nil 반환.
func ProvideMemberNewsReviewerClient(cliproxy config.CliproxyConfig, llmCfg config.LLMConfig, logger *slog.Logger) llm.Client {
	if !llmCfg.MemberNews.Enabled || !cliproxy.Enabled || cliproxy.APIKey == "" {
		return nil
	}

	model := llmCfg.MemberNews.ReviewerModel
	if model == "" {
		model = llmCfg.MemberNewsModel
	}
	if model == "" {
		model = cliproxy.Model
	}
	if cliproxy.BaseURL == "" || model == "" {
		logger.Warn("Consensus reviewer LLM configuration incomplete, skipping")
		return nil
	}

	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger,
		llm.WithSchemaName("member_news_review"),
		llm.WithTemperature(0.1),
		llm.WithWebSearch(false),
		llm.WithChatCompletions(),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	)
	logger.Info("Consensus reviewer LLM enabled", slog.String("model", model))
	return client
}

// ProvideMajorEventReviewerClient: major event consensus reviewer 전용 LLM 클라이언트.
func ProvideMajorEventReviewerClient(cliproxy config.CliproxyConfig, llmCfg config.LLMConfig, logger *slog.Logger) llm.Client {
	if !llmCfg.MajorEvent.Enabled || !cliproxy.Enabled || cliproxy.APIKey == "" {
		return nil
	}

	model := llmCfg.MajorEvent.ReviewerModel
	if model == "" {
		model = cliproxy.Model
	}
	if cliproxy.BaseURL == "" || model == "" {
		logger.Warn("Major event consensus reviewer LLM configuration incomplete, skipping")
		return nil
	}

	return llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger,
		llm.WithSchemaName("event_summary_review"),
		llm.WithWebSearch(false),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	)
}

// ProvideMajorEventAdjudicatorClient: major event consensus adjudicator 전용 LLM 클라이언트.
func ProvideMajorEventAdjudicatorClient(cliproxy config.CliproxyConfig, llmCfg config.LLMConfig, logger *slog.Logger) llm.Client {
	if !llmCfg.MajorEvent.Enabled || !cliproxy.Enabled || cliproxy.APIKey == "" {
		return nil
	}

	model := llmCfg.MajorEvent.AdjudicatorModel
	if model == "" {
		model = cliproxy.Model
	}
	if cliproxy.BaseURL == "" || model == "" {
		logger.Warn("Major event consensus adjudicator LLM configuration incomplete, skipping")
		return nil
	}

	return llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger,
		llm.WithSchemaName("event_summary"),
		llm.WithWebSearch(false),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	)
}

// ProvideMemberNewsAdjudicatorClient: consensus adjudicator 전용 LLM 클라이언트.
// consensus 비활성 또는 Cliproxy 비활성 시 nil 반환.
func ProvideMemberNewsAdjudicatorClient(cliproxy config.CliproxyConfig, llmCfg config.LLMConfig, logger *slog.Logger) llm.Client {
	if !llmCfg.MemberNews.Enabled || !cliproxy.Enabled || cliproxy.APIKey == "" {
		return nil
	}

	model := llmCfg.MemberNews.AdjudicatorModel
	if model == "" {
		model = llmCfg.MemberNewsModel
	}
	if model == "" {
		model = cliproxy.Model
	}
	if cliproxy.BaseURL == "" || model == "" {
		logger.Warn("Consensus adjudicator LLM configuration incomplete, skipping")
		return nil
	}

	opts := []llm.Option{
		llm.WithSchemaName("member_news_summary"),
		llm.WithWebSearch(false),
		llm.WithChatCompletions(),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
	}
	if llmCfg.MemberNewsTemperature > 0 {
		opts = append(opts, llm.WithTemperature(llmCfg.MemberNewsTemperature))
	}

	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger, opts...)
	logger.Info("Consensus adjudicator LLM enabled", slog.String("model", model))
	return client
}

// ProvideExaSearcher - Exa MCP 검색 클라이언트 생성 (비활성 시 nil)
func ProvideExaSearcher(cfg config.ExaConfig, logger *slog.Logger) majorevent.WebSearcher {
	if !cfg.Enabled || cfg.APIKey == "" {
		logger.Info("Exa search disabled")
		return nil
	}
	httpCfg := httpclient.DefaultConfig()
	httpCfg.Timeout = 15 * time.Second
	httpClient := httpclient.New(httpCfg)
	client := majorevent.NewExaMCPClient(cfg.Endpoint, cfg.APIKey, httpClient, logger)
	logger.Info("Exa search enabled", slog.String("endpoint", cfg.Endpoint))
	return client
}

// ProvideEventSummarizer - LLM 이벤트 요약 서비스 생성 (nil 허용)
func ProvideEventSummarizer(
	majorEventCfg config.ConsensusLLMConfig,
	llmClient majorevent.LLMClient,
	reviewerClient majorevent.LLMClient,
	adjudicatorClient majorevent.LLMClient,
	cacheSvc *cache.Service,
	searcher majorevent.WebSearcher,
	logger *slog.Logger,
) *majorevent.EventSummarizer {
	opts := make([]majorevent.SummarizerOption, 0, 1)
	if majorEventCfg.Enabled && reviewerClient != nil {
		opts = append(opts, majorevent.WithSummarizerConsensus(
			reviewerClient,
			adjudicatorClient,
			majorevent.SummarizerConsensusConfig{
				Enabled:             true,
				ConfidenceThreshold: majorEventCfg.Confidence,
				ReviewTimeout:       time.Duration(majorEventCfg.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(majorEventCfg.AdjudicateTimeout) * time.Second,
			},
		))
		logger.Info("Major event consensus summarizer enabled",
			slog.Float64("confidence_threshold", majorEventCfg.Confidence),
			slog.Int("review_timeout_sec", majorEventCfg.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", majorEventCfg.AdjudicateTimeout),
		)
	}
	return majorevent.NewEventSummarizer(llmClient, cacheSvc, searcher, logger, opts...)
}

// ProvideMajorEventRepository - 대형 행사 구독 저장소 생성
func ProvideMajorEventRepository(
	ctx context.Context,
	postgres *database.PostgresService,
	logger *slog.Logger,
	autoPrepareSchema bool,
) *majorevent.Repository {
	repo := majorevent.NewRepository(postgres, logger)
	if autoPrepareSchema {
		if err := repo.CreateTable(ctx); err != nil {
			logger.Error("Failed to create major_event_subscriptions table", slog.String("error", err.Error()))
		}
	}
	return repo
}

// ProvideMajorEventService - 대형 행사 서비스 생성
func ProvideMajorEventService(
	logger *slog.Logger,
) *majorevent.Service {
	cfg := httpclient.DefaultConfig()
	cfg.Timeout = constants.MajorEventConfig.RequestTimeout
	cfg.ResponseHeaderTimeout = 20 * time.Second
	cfg.HTTP2ReadIdleTimeout = 30 * time.Second
	cfg.HTTP2PingTimeout = 5 * time.Second
	httpClient := httpclient.New(cfg)
	return majorevent.NewService(
		httpClient,
		majorevent.WithRSSURL(constants.MajorEventConfig.EventRSSURL),
		majorevent.WithLogger(logger),
	)
}

// ProvideMajorEventHTTPClient - 대형 행사 HTTP 클라이언트 생성
func ProvideMajorEventHTTPClient() *http.Client {
	cfg := httpclient.DefaultConfig()
	cfg.Timeout = constants.MajorEventConfig.RequestTimeout
	cfg.ResponseHeaderTimeout = 20 * time.Second
	cfg.HTTP2ReadIdleTimeout = 30 * time.Second
	cfg.HTTP2PingTimeout = 5 * time.Second
	return httpclient.New(cfg)
}

// ProvideMemberNewsRepository: 구독 멤버 뉴스 저장소 생성
func ProvideMemberNewsRepository(
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) *membernews.Repository {
	return membernews.NewRepository(postgres, cacheSvc, logger)
}

// ProvideMemberNewsService: 구독 멤버 뉴스 서비스 생성
func ProvideMemberNewsService(
	ctx context.Context,
	repo *membernews.Repository,
	llmClient llm.Client,
	reviewerClient llm.Client,
	adjudicatorClient llm.Client,
	searcher majorevent.WebSearcher,
	membersData domain.MemberDataProvider,
	memberNewsCfg config.ConsensusLLMConfig,
	logger *slog.Logger,
) *membernews.Service {
	allowlistPath := resolveMemberNewsXAllowlistPath()
	validator, err := membernews.NewSourceValidator(allowlistPath, membersData, logger)
	if err != nil {
		logger.Warn("Failed to load member news x allowlist, fallback to empty allowlist",
			slog.String("path", allowlistPath),
			slog.String("error", err.Error()),
		)
		validator, _ = membernews.NewSourceValidator("", membersData, logger)
	}

	adaptedSearcher := &memberNewsSearcherAdapter{base: searcher}
	baseSummarizer := membernews.NewSummarizer(llmClient, adaptedSearcher, validator, logger)

	var summarizer membernews.Summarizer = baseSummarizer
	if memberNewsCfg.Enabled && reviewerClient != nil {
		summarizer = membernews.NewConsensusSummarizer(
			baseSummarizer, reviewerClient, adjudicatorClient, validator,
			membernews.ConsensusConfig{
				ConfidenceThreshold: memberNewsCfg.Confidence,
				ReviewTimeout:       time.Duration(memberNewsCfg.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(memberNewsCfg.AdjudicateTimeout) * time.Second,
			},
			logger,
		)
		logger.Info("Consensus summarizer enabled",
			slog.Float64("confidence_threshold", memberNewsCfg.Confidence),
			slog.Int("review_timeout_sec", memberNewsCfg.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", memberNewsCfg.AdjudicateTimeout),
		)
	}

	service := membernews.NewService(repo, summarizer, validator, membersData, logger)
	if warmErr := service.WarmupSubscriptionCache(ctx); warmErr != nil {
		logger.Warn("Member news subscription warmup failed", slog.String("error", warmErr.Error()))
	}
	return service
}

// ProvideYouTubeStatsRepository - YouTube 통계 저장소 생성
func ProvideYouTubeStatsRepository(
	postgres *database.PostgresService,
	logger *slog.Logger,
) *youtube.StatsRepository {
	return youtube.NewYouTubeStatsRepository(postgres, logger)
}

// ProvideYouTubeStack - YouTube 서비스 스택 생성
func ProvideYouTubeStack(
	ctx context.Context,
	ytCfg config.YouTubeConfig,
	scraperCfg config.ScraperConfig,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	members *member.ServiceAdapter,
	statsRepo *youtube.StatsRepository,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *YouTubeStack {
	if !ytCfg.EnableQuotaBuilding || ytCfg.APIKey == "" {
		logger.Info("YouTube quota building disabled; stats repository only")
		return &YouTubeStack{StatsRepo: statsRepo}
	}

	svc, err := youtube.NewYouTubeService(ctx, ytCfg.APIKey, cacheSvc, scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}, sharedRL, logger)
	if err != nil {
		logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
		return &YouTubeStack{StatsRepo: statsRepo}
	}

	scheduler := youtube.NewScheduler(svc, holodexSvc, cacheSvc, statsRepo, members, alarmSvc, irisClient, formatter, logger)
	logger.Info("YouTube quota building enabled",
		slog.String("mode", "API Key"),
		slog.Int("daily_target", 9192))

	return &YouTubeStack{
		Service:   svc,
		Scheduler: scheduler,
		StatsRepo: statsRepo,
	}
}

// ProvideDeliveryLocker - 분산 락 인스턴스 생성
func ProvideDeliveryLocker(cacheSvc *cache.Service, logger *slog.Logger) delivery.NotificationLocker {
	return delivery.NewLocker(cacheSvc, logger)
}

// ProvideOutboxRepository - 알림 delivery outbox 저장소 생성
func ProvideOutboxRepository(postgres *database.PostgresService, logger *slog.Logger) *delivery.OutboxRepository {
	return delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
}

// ProvideDeliveryDispatcher - outbox 발송 워커 생성
func ProvideDeliveryDispatcher(repo *delivery.OutboxRepository, sender delivery.MessageSender, logger *slog.Logger) *delivery.Dispatcher {
	return delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
}

// ProvideAlarmQueueDispatcher: 알림 큐 디스패처 생성 (비활성 시 nil)
func ProvideAlarmQueueDispatcher(
	enabled bool,
	cacheSvc *cache.Service,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) *notification.AlarmQueueDispatcher {
	if !enabled {
		logger.Info("Alarm queue consumer disabled by config")
		return nil
	}
	return notification.NewAlarmQueueDispatcher(
		cacheSvc.GetClient(),
		alarmSvc,
		irisClient,
		formatter,
		logger,
	)
}

// ProvideAlarmWorkerPool - 알림 처리용 워커풀 생성
func ProvideAlarmWorkerPool() (*workerpool.Pool, error) {
	cfg := workerpool.DefaultConfig()
	cfg.Size = 10
	pool, err := workerpool.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm worker pool: %w", err)
	}
	return pool, nil
}

// ProvideSettingsService - 설정 서비스 생성
func ProvideSettingsService(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) *settings.Service {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}, logger)
}

// ProvideMessageStack - 메시지 어댑터 및 포매터 생성
func ProvideMessageStack(botPrefix string, renderer *template.Renderer) *MessageStack {
	msgAdapter, formatter := bootstrap.NewMessageStack(botPrefix, renderer)
	return &MessageStack{
		Adapter:   msgAdapter,
		Formatter: formatter,
	}
}

// ProvideScraperScheduler - YouTube HTML 스크래퍼 기반 폴러 스케줄러 생성
// 멤버 채널 목록을 조회하여 모든 폴러를 스케줄러에 등록한다.
func ProvideScraperScheduler(
	postgres *database.PostgresService,
	membersData domain.MemberDataProvider,
	intervals PollerIntervals,
	communityKeywords []string,
	proxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) *poller.Scheduler {
	// 스크래퍼 클라이언트 생성 (공유 RateLimiter 주입)
	scraperClient := scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheSvc),
	)
	db := postgres.GetGormDB()

	// 폴러 생성
	videosPoller := poller.NewVideosPoller(scraperClient, db, 10)
	shortsPoller := poller.NewShortsPoller(scraperClient, db, 10)
	communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords)
	statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
	livePoller := poller.NewLivePoller(scraperClient, db)

	// 스케줄러 생성 (RequestInterval=0: 외부 sharedRL에 rate limiting 위임)
	scheduler := poller.NewScheduler(poller.SchedulerConfig{
		WorkerCount:     2,
		RequestInterval: 0,
	})

	// 모든 멤버 채널에 대해 폴러 등록
	members := membersData.GetAllMembers()
	for _, m := range members {
		if m.IsGraduated {
			continue // 졸업 멤버 제외
		}

		channelID := m.ChannelID

		// 영상 폴러 (일반 우선순위)
		scheduler.Register(channelID, videosPoller, poller.PriorityNormal, intervals.Videos)

		// 쇼츠 폴러 (낮은 우선순위)
		scheduler.Register(channelID, shortsPoller, poller.PriorityLow, intervals.Shorts)

		// 커뮤니티 폴러 (낮은 우선순위)
		scheduler.Register(channelID, communityPoller, poller.PriorityLow, intervals.Community)

		// 채널 통계 폴러 (낮은 우선순위)
		scheduler.Register(channelID, statsPoller, poller.PriorityLow, intervals.Stats)

		// 라이브 폴러 (높은 우선순위)
		scheduler.Register(channelID, livePoller, poller.PriorityHigh, intervals.Live)
	}

	logger.Info("Scraper scheduler initialized",
		slog.Int("members", len(members)),
		slog.Int("total_jobs", len(members)*5)) // 5 pollers per member

	return scheduler
}
