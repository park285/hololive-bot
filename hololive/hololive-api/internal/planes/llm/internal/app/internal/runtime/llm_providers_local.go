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

	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// ProvideLLMCostTracker는 6개 provider가 공유할 월 토큰 cost tracker를 만든다. ceiling<=0 또는 cache
// 미가용이면 true-nil interface를 반환해 관측을 비활성한다(typed-nil interface 함정 회피).
func ProvideLLMCostTracker(cacheClient cache.Client, monthlyCeiling int64, logger *slog.Logger) llm.CostTracker {
	if tracker := llm.NewValkeyCostCeiling(cacheClient, monthlyCeiling, logger); tracker != nil {
		return tracker
	}
	return nil
}

// ProvideMajorEventLLMClient - MajorEvent 전용 LLM 클라이언트 생성 (비활성 시 nil)
func ProvideMajorEventLLMClient(cliproxy config.CliproxyConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
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
		llm.WithCostTracker(tracker),
	)
	logger.Info("Cliproxy LLM enabled for event summaries (responses + web_search, chat fallback)",
		slog.String("model", cliproxy.Model),
		slog.String("reasoning_effort", cliproxy.ReasoningEffort))
	return client
}

func ProvideMemberNewsLLMClient(cliproxy config.CliproxyConfig, llmConfig *config.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	if !cliproxy.Enabled || cliproxy.APIKey == "" {
		logger.Info("Member news LLM disabled")
		return nil
	}

	model := llmConfig.MemberNewsModel
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
		llm.WithCostTracker(tracker),
	}
	if llmConfig.MemberNewsTemperature > 0 {
		opts = append(opts, llm.WithTemperature(llmConfig.MemberNewsTemperature))
	}

	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, model, logger, opts...)
	tempApplied := llmConfig.MemberNewsTemperature > 0
	logger.Info("Member news LLM enabled",
		slog.String("model", model),
		slog.Bool("temperature_applied", tempApplied),
		slog.Float64("temperature", llmConfig.MemberNewsTemperature),
	)
	return client
}

// consensusClientSpec는 reviewer/adjudicator client 빌더가 함수별로 달라지는 축(enabled
// 결정, model fallback 결과, incomplete 경고 문구, NewClient 옵션, 성공 로그)만 주입받는다.
// guard·completeness·NewClient boilerplate는 buildConsensusLLMClient가 처리한다.
type consensusClientSpec struct {
	enabled        bool
	model          string
	incompleteWarn string
	options        []llm.Option
	onSuccess      func(model string)
}

func buildConsensusLLMClient(cliproxy config.CliproxyConfig, logger *slog.Logger, spec consensusClientSpec) llm.Client {
	if !spec.enabled || !cliproxy.Enabled || cliproxy.APIKey == "" {
		return nil
	}
	if cliproxy.BaseURL == "" || spec.model == "" {
		logger.Warn(spec.incompleteWarn)
		return nil
	}

	client := llm.NewClient(cliproxy.BaseURL, cliproxy.APIKey, spec.model, logger, spec.options...)
	if spec.onSuccess != nil {
		spec.onSuccess(spec.model)
	}
	return client
}

// consensus 비활성 또는 Cliproxy 비활성 시 nil 반환.
func ProvideMemberNewsReviewerClient(cliproxy config.CliproxyConfig, llmConfig *config.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	model := llmConfig.MemberNews.ReviewerModel
	if model == "" {
		model = llmConfig.MemberNewsModel
	}
	if model == "" {
		model = cliproxy.Model
	}

	return buildConsensusLLMClient(cliproxy, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		incompleteWarn: "Consensus reviewer LLM configuration incomplete, skipping",
		options: []llm.Option{
			llm.WithSchemaName("member_news_review"),
			llm.WithTemperature(0.1),
			llm.WithWebSearch(false),
			llm.WithChatCompletions(),
			llm.WithReasoningEffort(cliproxy.ReasoningEffort),
			llm.WithCostTracker(tracker),
		},
		onSuccess: func(m string) {
			logger.Info("Consensus reviewer LLM enabled", slog.String("model", m))
		},
	})
}

func ProvideMajorEventReviewerClient(cliproxy config.CliproxyConfig, llmConfig *config.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	model := llmConfig.MajorEvent.ReviewerModel
	if model == "" {
		model = cliproxy.Model
	}

	return buildConsensusLLMClient(cliproxy, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          model,
		incompleteWarn: "Major event consensus reviewer LLM configuration incomplete, skipping",
		options: []llm.Option{
			llm.WithSchemaName("event_summary_review"),
			llm.WithWebSearch(false),
			llm.WithReasoningEffort(cliproxy.ReasoningEffort),
			llm.WithCostTracker(tracker),
		},
	})
}

func ProvideMajorEventAdjudicatorClient(cliproxy config.CliproxyConfig, llmConfig *config.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	model := llmConfig.MajorEvent.AdjudicatorModel
	if model == "" {
		model = cliproxy.Model
	}

	return buildConsensusLLMClient(cliproxy, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          model,
		incompleteWarn: "Major event consensus adjudicator LLM configuration incomplete, skipping",
		options: []llm.Option{
			llm.WithSchemaName("event_summary"),
			llm.WithWebSearch(false),
			llm.WithReasoningEffort(cliproxy.ReasoningEffort),
			llm.WithCostTracker(tracker),
		},
	})
}

// consensus 비활성 또는 Cliproxy 비활성 시 nil 반환.
func ProvideMemberNewsAdjudicatorClient(cliproxy config.CliproxyConfig, llmConfig *config.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &config.LLMConfig{}
	}
	model := llmConfig.MemberNews.AdjudicatorModel
	if model == "" {
		model = llmConfig.MemberNewsModel
	}
	if model == "" {
		model = cliproxy.Model
	}

	opts := []llm.Option{
		llm.WithSchemaName("member_news_summary"),
		llm.WithWebSearch(false),
		llm.WithChatCompletions(),
		llm.WithReasoningEffort(cliproxy.ReasoningEffort),
		llm.WithCostTracker(tracker),
	}
	if llmConfig.MemberNewsTemperature > 0 {
		opts = append(opts, llm.WithTemperature(llmConfig.MemberNewsTemperature))
	}

	return buildConsensusLLMClient(cliproxy, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		incompleteWarn: "Consensus adjudicator LLM configuration incomplete, skipping",
		options:        opts,
		onSuccess: func(m string) {
			logger.Info("Consensus adjudicator LLM enabled", slog.String("model", m))
		},
	})
}
