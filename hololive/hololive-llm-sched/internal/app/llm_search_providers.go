package app

import (
	"log/slog"
	"strings"
	"time"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

func provideExaSearcher(cfg config.ExaConfig, logger *slog.Logger) sharedmodel.WebSearcher {
	if logger == nil {
		logger = slog.Default()
	}

	if !cfg.Enabled || strings.TrimSpace(cfg.APIKey) == "" {
		logger.Info("Exa search disabled")
		return nil
	}

	httpClient := httputil.NewExternalAPIClient(15 * time.Second)
	client := mesummarizer.NewExaMCPClient(cfg.Endpoint, cfg.APIKey, httpClient, logger)
	logger.Info("Exa search enabled", slog.String("endpoint", cfg.Endpoint))
	return client
}

func provideEventSummarizer(
	majorEventCfg config.ConsensusLLMConfig,
	llmClient mesummarizer.LLMClient,
	reviewerClient mesummarizer.LLMClient,
	adjudicatorClient mesummarizer.LLMClient,
	cacheSvc cache.Client,
	searcher sharedmodel.WebSearcher,
	logger *slog.Logger,
) *mesummarizer.EventSummarizer {
	if logger == nil {
		logger = slog.Default()
	}

	opts := make([]mesummarizer.SummarizerOption, 0, 1)
	if majorEventCfg.Enabled && reviewerClient != nil {
		opts = append(opts, mesummarizer.WithSummarizerConsensus(
			reviewerClient,
			adjudicatorClient,
			mesummarizer.SummarizerConsensusConfig{
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

	return mesummarizer.NewEventSummarizer(llmClient, cacheSvc, searcher, logger, opts...)
}
