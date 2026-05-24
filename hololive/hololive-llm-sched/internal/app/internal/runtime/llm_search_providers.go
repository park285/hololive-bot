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
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func provideExaSearcher(exaConfig config.ExaConfig, logger *slog.Logger) sharedmodel.WebSearcher {
	if logger == nil {
		logger = slog.Default()
	}

	if !exaConfig.Enabled || strings.TrimSpace(exaConfig.APIKey) == "" {
		logger.Info("Exa search disabled")
		return nil
	}

	httpClient := httputil.NewExternalAPIClient(15 * time.Second)
	client := mesummarizer.NewExaMCPClient(exaConfig.Endpoint, exaConfig.APIKey, httpClient, logger)
	logger.Info("Exa search enabled", slog.String("endpoint", exaConfig.Endpoint))
	return client
}

func buildMajorEventSummarizer(exaConfig *config.LLMSchedulerConfig, cacheClient cache.Client, logger *slog.Logger) *mesummarizer.EventSummarizer {
	majorEventLLMClient := ProvideMajorEventLLMClient(exaConfig.Cliproxy, logger)
	majorEventReviewer := ProvideMajorEventReviewerClient(exaConfig.Cliproxy, exaConfig.LLM, logger)
	majorEventAdjudicator := ProvideMajorEventAdjudicatorClient(exaConfig.Cliproxy, exaConfig.LLM, logger)
	exaSearcher := provideExaSearcher(exaConfig.Exa, logger)
	return provideEventSummarizer(exaConfig.LLM.MajorEvent, majorEventLLMClient, majorEventReviewer, majorEventAdjudicator, cacheClient, exaSearcher, logger)
}

func provideEventSummarizer(
	majorEventConfig config.ConsensusLLMConfig,
	llmClient mesummarizer.LLMClient,
	reviewerClient mesummarizer.LLMClient,
	adjudicatorClient mesummarizer.LLMClient,
	cacheClient cache.Client,
	searcher sharedmodel.WebSearcher,
	logger *slog.Logger,
) *mesummarizer.EventSummarizer {
	if logger == nil {
		logger = slog.Default()
	}

	opts := make([]mesummarizer.SummarizerOption, 0, 1)
	if majorEventConfig.Enabled && reviewerClient != nil {
		opts = append(opts, mesummarizer.WithSummarizerConsensus(
			reviewerClient,
			adjudicatorClient,
			mesummarizer.SummarizerConsensusConfig{
				Enabled:             true,
				ConfidenceThreshold: majorEventConfig.Confidence,
				ReviewTimeout:       time.Duration(majorEventConfig.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(majorEventConfig.AdjudicateTimeout) * time.Second,
			},
		))
		logger.Info("Major event consensus summarizer enabled",
			slog.Float64("confidence_threshold", majorEventConfig.Confidence),
			slog.Int("review_timeout_sec", majorEventConfig.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", majorEventConfig.AdjudicateTimeout),
		)
	}

	return mesummarizer.NewEventSummarizer(llmClient, cacheClient, searcher, logger, opts...)
}
