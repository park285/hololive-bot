package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func initMemberNewsService(
	ctx context.Context,
	cliproxy config.CliproxyConfig,
	llmCfg config.LLMConfig,
	exaCfg config.ExaConfig,
	postgres database.Client,
	cacheSvc cache.Client,
	membersData member.DataProvider,
	logger *slog.Logger,
) *membernews.Service {
	repo := membernews.NewRepository(postgres, cacheSvc, logger)
	llmClient := ProvideMemberNewsLLMClient(cliproxy, llmCfg, logger)
	reviewer := ProvideMemberNewsReviewerClient(cliproxy, llmCfg, logger)
	adjudicator := ProvideMemberNewsAdjudicatorClient(cliproxy, llmCfg, logger)

	searcher := provideExaSearcher(exaCfg, logger)

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
	if llmCfg.MemberNews.Enabled && reviewer != nil {
		summarizer = membernews.NewConsensusSummarizer(
			baseSummarizer, reviewer, adjudicator, validator,
			membernews.ConsensusConfig{
				ConfidenceThreshold: llmCfg.MemberNews.Confidence,
				ReviewTimeout:       time.Duration(llmCfg.MemberNews.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(llmCfg.MemberNews.AdjudicateTimeout) * time.Second,
			},
			logger,
		)
		logger.Info("Consensus summarizer enabled",
			slog.Float64("confidence_threshold", llmCfg.MemberNews.Confidence),
			slog.Int("review_timeout_sec", llmCfg.MemberNews.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", llmCfg.MemberNews.AdjudicateTimeout),
		)
	}

	service := membernews.NewService(repo, summarizer, validator, membersData, logger)
	if warmErr := service.WarmupSubscriptionCache(ctx); warmErr != nil {
		logger.Warn("Member news subscription warmup failed", slog.String("error", warmErr.Error()))
	}

	return service
}

func resolveMemberNewsXAllowlistPath() string {
	if envPath := os.Getenv("MEMBER_NEWS_X_ALLOWLIST_PATH"); strings.TrimSpace(envPath) != "" {
		return envPath
	}

	candidates := []string{
		filepath.Join("configs", "hololive_official_x_accounts.json"),
		filepath.Join("..", "configs", "hololive_official_x_accounts.json"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// memberNewsSearcherAdapter: majorevent/summarizer.WebSearcher를 membernews.WebSearcher로 변환
type memberNewsSearcherAdapter struct {
	base mesummarizer.WebSearcher
}

func (a *memberNewsSearcherAdapter) Search(ctx context.Context, query string) ([]membernews.SearchResult, error) {
	if a == nil || a.base == nil {
		return nil, nil
	}
	results, err := a.base.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search member news context: %w", err)
	}

	converted := make([]membernews.SearchResult, 0, len(results))
	converted = append(converted, results...)
	return converted, nil
}
