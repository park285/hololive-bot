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

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"
	mnsummarizer "github.com/kapu/hololive-llm-sched/internal/service/membernews/summarizer"

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
	cacheClient cache.Client,
	membersData member.DataProvider,
	logger *slog.Logger,
) *membernews.Service {
	repository := membernews.NewRepository(postgres, cacheClient, logger)
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

	baseSummarizer := mnsummarizer.NewSummarizer(llmClient, searcher, validator, logger)

	var summarizer membernews.Summarizer = baseSummarizer
	if llmCfg.MemberNews.Enabled && reviewer != nil {
		summarizer = mnsummarizer.NewConsensusSummarizer(
			baseSummarizer, reviewer, adjudicator, validator,
			consensus.Config{
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

	service := membernews.NewService(repository, summarizer, validator, membersData, logger)
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
