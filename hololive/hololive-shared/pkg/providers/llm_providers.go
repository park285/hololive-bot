package providers

import (
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httpclient"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/llm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
)

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
