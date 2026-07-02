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

package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	mnsummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/summarizer"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/park285/shared-go/pkg/envutil"
)

func initMemberNewsService(
	ctx context.Context,
	cliproxy config.CliproxyConfig,
	llmConfig *config.LLMConfig,
	exaConfig config.ExaConfig,
	postgres database.Client,
	cacheClient cache.Client,
	membersData member.DataProvider,
	logger *slog.Logger,
) *membernews.Service {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	repository := membernews.NewRepository(postgres, cacheClient, logger)
	costTracker := ProvideLLMCostTracker(cacheClient, llmConfig.MonthlyTokenCeiling, logger)
	llmClient := ProvideMemberNewsLLMClient(cliproxy, llmConfig, costTracker, logger)
	reviewer := ProvideMemberNewsReviewerClient(cliproxy, llmConfig, costTracker, logger)
	adjudicator := ProvideMemberNewsAdjudicatorClient(cliproxy, llmConfig, costTracker, logger)

	searcher := provideExaSearcher(exaConfig, logger)

	validator := initMemberNewsSourceValidator(membersData, logger)

	baseSummarizer := mnsummarizer.NewSummarizer(llmClient, searcher, validator, logger)

	var summarizer membernews.Summarizer = baseSummarizer
	if llmConfig.MemberNews.Enabled && reviewer != nil {
		summarizer = mnsummarizer.NewConsensusSummarizer(
			baseSummarizer, reviewer, adjudicator, validator,
			consensus.Config{
				ConfidenceThreshold: llmConfig.MemberNews.Confidence,
				ReviewTimeout:       time.Duration(llmConfig.MemberNews.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(llmConfig.MemberNews.AdjudicateTimeout) * time.Second,
			},
			logger,
		)
		logger.Info("Consensus summarizer enabled",
			slog.Float64("confidence_threshold", llmConfig.MemberNews.Confidence),
			slog.Int("review_timeout_sec", llmConfig.MemberNews.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", llmConfig.MemberNews.AdjudicateTimeout),
		)
	}

	service := membernews.NewService(repository, summarizer, validator, membersData, logger)
	if warmErr := service.WarmupSubscriptionCache(ctx); warmErr != nil {
		logger.Warn("Member news subscription warmup failed", slog.String("error", warmErr.Error()))
	}

	return service
}

func initMemberNewsSourceValidator(membersData member.DataProvider, logger *slog.Logger) *membernews.SourceValidator {
	allowlistPath := resolveMemberNewsXAllowlistPath()
	validator, err := membernews.NewSourceValidator(allowlistPath, membersData, logger)
	if err == nil {
		return validator
	}

	logger.Warn("Failed to load member news x allowlist, fallback to empty allowlist",
		slog.String("path", allowlistPath),
		slog.String("error", err.Error()),
	)
	validator, err = membernews.NewSourceValidator("", membersData, logger)
	if err != nil {
		logger.Warn("Failed to initialize empty member news x allowlist",
			slog.String("error", err.Error()),
		)
	}
	return validator
}

func resolveMemberNewsXAllowlistPath() string {
	if envPath := envutil.StringRaw("MEMBER_NEWS_X_ALLOWLIST_PATH", ""); strings.TrimSpace(envPath) != "" {
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
