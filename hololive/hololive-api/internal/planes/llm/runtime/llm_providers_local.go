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
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// errSelectedLLMProviderDisabled와 errConsensusLLMDisabled는 기능이 설정으로 꺼져 있음을 알리는 sentinel이다.
// 초기화 실패와 구분해야 기능 활성 시의 실패만 기동 실패로 올릴 수 있다(stack audit B5).
var (
	errSelectedLLMProviderDisabled = errors.New("selected LLM provider is disabled")
	errConsensusLLMDisabled        = errors.New("consensus LLM is disabled")
)

type selectedLLMProvider struct {
	name           string
	baseURL        string
	apiKey         string
	defaultModel   string
	reasoningLevel string
}

type providerClientSpec struct {
	model           string
	schemaName      string
	temperature     float64
	webSearch       bool
	chatCompletions bool
	preset          bool
}

type consensusClientSpec struct {
	enabled        bool
	model          string
	schemaName     string
	temperature    float64
	chatCompletion bool
	successMessage string
	preset         bool
}

// ProvideLLMCostTracker는 토큰 사용량을 항상 메트릭으로 기록하는 recorder를 돌려준다.
// 월 상한과 Valkey 월 카운터는 DEC-20260926-hololive-llm-token-ceiling-retirement로 퇴역했다.
func ProvideLLMCostTracker() llm.CostTracker {
	return llm.NewTokenMetricsRecorder()
}

func ProvideMajorEventLLMClient(provider settings.LLMProviderConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	client, model, _, err := buildProviderClient(provider, tracker, logger, providerClientSpec{
		schemaName: "event_summary",
		webSearch:  true,
	})
	if err != nil {
		logProviderInitializationError(logger, "Event summary LLM", err)

		return nil
	}

	logger.Info("Event summary LLM enabled",
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.String("model", model),
		slog.String("reasoning_level", selectedProviderReasoningLevel(provider)),
		slog.Bool("web_search", true),
	)

	return client
}

// ProvideMemberNewsLLMClient는 member news 요약 client를 만든다. 선택한 provider가 꺼져 있으면
// errSelectedLLMProviderDisabled를 감싼 오류로 기능 비활성을 알리고, 그 밖의 초기화 실패는 기동 실패 대상 오류다.
func ProvideMemberNewsLLMClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) (llm.Client, error) {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	client, model, temperatureApplied, err := buildProviderClient(provider, tracker, logger, providerClientSpec{
		model:           llmConfig.MemberNewsModel,
		schemaName:      "member_news_summary",
		temperature:     llmConfig.MemberNewsTemperature,
		chatCompletions: true,
		preset:          true,
	})
	if errors.Is(err, errSelectedLLMProviderDisabled) {
		logger.Info("Member news LLM disabled")
	}

	if err != nil {
		return nil, fmt.Errorf("initialize member news LLM client: %w", err)
	}

	logger.Info("Member news LLM enabled",
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.String("model", model),
		slog.Bool("temperature_applied", temperatureApplied),
		slog.Float64("temperature", llmConfig.MemberNewsTemperature),
	)

	return client, nil
}

// ProvideMemberNewsReviewerClient는 consensus가 켜져 있을 때만 reviewer client를 만든다. 설정으로 consensus나
// provider가 꺼져 있으면 비활성 sentinel을 감싼 오류를, 그 밖의 초기화 실패는 기동 실패 대상 오류를 돌려준다.
func ProvideMemberNewsReviewerClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) (llm.Client, error) {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	model := cmp.Or(llmConfig.MemberNews.ReviewerModel, llmConfig.MemberNewsModel)

	client, err := buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		schemaName:     "member_news_review",
		temperature:    0.1,
		chatCompletion: true,
		successMessage: "Consensus reviewer LLM enabled",
		preset:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize member news consensus reviewer: %w", err)
	}

	return client, nil
}

func ProvideMajorEventReviewerClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	client, err := buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          llmConfig.MajorEvent.ReviewerModel,
		schemaName:     "event_summary_review",
		successMessage: "Major event consensus reviewer LLM enabled",
		preset:         true,
	})
	if err != nil {
		logConsensusInitializationWarning(logger, "Major event consensus reviewer LLM configuration incomplete, skipping", provider, err)

		return nil
	}

	return client
}

func ProvideMajorEventAdjudicatorClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	client, err := buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          llmConfig.MajorEvent.AdjudicatorModel,
		schemaName:     "event_summary",
		successMessage: "Major event consensus adjudicator LLM enabled",
	})
	if err != nil {
		logConsensusInitializationWarning(logger, "Major event consensus adjudicator LLM configuration incomplete, skipping", provider, err)

		return nil
	}

	return client
}

// ProvideMemberNewsAdjudicatorClient는 consensus가 켜져 있을 때만 adjudicator client를 만든다. 오류 계약은
// ProvideMemberNewsReviewerClient와 같다.
func ProvideMemberNewsAdjudicatorClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) (llm.Client, error) {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	model := cmp.Or(llmConfig.MemberNews.AdjudicatorModel, llmConfig.MemberNewsModel)

	client, err := buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		schemaName:     "member_news_summary",
		temperature:    llmConfig.MemberNewsTemperature,
		chatCompletion: true,
		successMessage: "Consensus adjudicator LLM enabled",
		preset:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize member news consensus adjudicator: %w", err)
	}

	return client, nil
}

// buildConsensusLLMClient는 consensus나 provider가 꺼져 있으면 비활성 sentinel을 감싼 오류를 돌려준다.
// 오류를 기동 실패로 올릴지는 기능별 호출자가 정한다.
func buildConsensusLLMClient(provider settings.LLMProviderConfig, tracker llm.CostTracker, logger *slog.Logger, spec consensusClientSpec) (llm.Client, error) {
	if !spec.enabled {
		return nil, errConsensusLLMDisabled
	}

	client, model, _, err := buildProviderClient(provider, tracker, logger, providerClientSpec{
		model:           spec.model,
		schemaName:      spec.schemaName,
		temperature:     spec.temperature,
		webSearch:       false,
		chatCompletions: spec.chatCompletion,
		preset:          spec.preset,
	})
	if err != nil {
		return nil, fmt.Errorf("build consensus provider client: %w", err)
	}

	logger.Info(spec.successMessage,
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.String("model", model),
	)

	return client, nil
}

func isLLMFeatureDisabled(err error) bool {
	return errors.Is(err, errSelectedLLMProviderDisabled) || errors.Is(err, errConsensusLLMDisabled)
}

// major event consensus는 B5 범위 밖이라 기존대로 초기화 실패를 경고로 남기고 consensus 없이 진행한다.
func logConsensusInitializationWarning(logger *slog.Logger, message string, provider settings.LLMProviderConfig, err error) {
	if isLLMFeatureDisabled(err) {
		return
	}

	logger.Warn(message,
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.Any("error", err),
	)
}

func buildProviderClient(provider settings.LLMProviderConfig, tracker llm.CostTracker, logger *slog.Logger, spec providerClientSpec) (llm.Client, string, bool, error) {
	selected, err := selectLLMProvider(provider)
	if err != nil {
		return nil, "", false, fmt.Errorf("select LLM provider: %w", err)
	}

	model := cmp.Or(strings.TrimSpace(spec.model), selected.defaultModel)
	if model == "" {
		return nil, "", false, errors.New("selected LLM provider model is empty")
	}

	opts := []llm.Option{
		llm.WithSchemaName(spec.schemaName),
		llm.WithWebSearch(spec.webSearch),
		llm.WithReasoningEffort(selected.reasoningLevel),
		llm.WithCostTracker(tracker),
	}

	if selected.name == settings.LLMProviderGemini {
		client, buildErr := buildGeminiProviderClient(selected, model, logger, opts)
		if buildErr != nil {
			return nil, "", false, fmt.Errorf("build gemini provider client: %w", buildErr)
		}

		return client, model, false, nil
	}

	client, selectedModel, temperatureApplied, buildErr := buildCliproxyProviderClient(selected, model, logger, opts, spec)
	if buildErr != nil {
		return nil, "", false, fmt.Errorf("build cliproxy provider client: %w", buildErr)
	}

	return client, selectedModel, temperatureApplied, nil
}

func buildGeminiProviderClient(selected selectedLLMProvider, model string, logger *slog.Logger, opts []llm.Option) (llm.Client, error) {
	client, err := llm.NewGeminiClient(selected.baseURL, selected.apiKey, model, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("initialize gemini LLM client: %w", err)
	}

	return client, nil
}

func buildCliproxyProviderClient(selected selectedLLMProvider, model string, logger *slog.Logger, opts []llm.Option, spec providerClientSpec) (llm.Client, string, bool, error) {
	if spec.chatCompletions {
		opts = append(opts, llm.WithChatCompletions())
	}

	opts, temperatureApplied := appendSupportedTemperature(opts, model, spec.temperature)
	if spec.preset {
		client, err := llm.NewPresetClient(selected.baseURL, selected.apiKey, model, logger, opts...)
		if err != nil {
			return nil, "", false, fmt.Errorf("initialize cliproxy preset LLM client: %w", err)
		}

		return client, model, temperatureApplied, nil
	}

	client, err := llm.NewClient(selected.baseURL, selected.apiKey, model, logger, opts...)
	if err != nil {
		return nil, "", false, fmt.Errorf("initialize cliproxy LLM client: %w", err)
	}

	return client, model, temperatureApplied, nil
}

func selectLLMProvider(config settings.LLMProviderConfig) (selectedLLMProvider, error) {
	switch normalizedProviderName(config.Name) {
	case settings.LLMProviderGemini:
		provider, err := selectGeminiProvider(config.Gemini)
		if err != nil {
			return selectedLLMProvider{}, fmt.Errorf("select gemini provider: %w", err)
		}

		return provider, nil
	case settings.LLMProviderCliproxy:
		provider, err := selectCliproxyProvider(config.Cliproxy)
		if err != nil {
			return selectedLLMProvider{}, fmt.Errorf("select cliproxy provider: %w", err)
		}

		return provider, nil
	default:
		return selectedLLMProvider{}, errors.New("selected LLM provider is unsupported")
	}
}

func selectGeminiProvider(config settings.GeminiConfig) (selectedLLMProvider, error) {
	if !config.Enabled || strings.TrimSpace(config.APIKey) == "" {
		return selectedLLMProvider{}, errSelectedLLMProviderDisabled
	}

	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.Model) == "" {
		return selectedLLMProvider{}, errors.New("selected gemini provider configuration is incomplete")
	}

	return selectedLLMProvider{
		name:           settings.LLMProviderGemini,
		baseURL:        config.BaseURL,
		apiKey:         config.APIKey,
		defaultModel:   config.Model,
		reasoningLevel: config.ThinkingLevel,
	}, nil
}

func selectCliproxyProvider(config settings.CliproxyConfig) (selectedLLMProvider, error) {
	if !config.Enabled || strings.TrimSpace(config.APIKey) == "" {
		return selectedLLMProvider{}, errSelectedLLMProviderDisabled
	}

	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.Model) == "" {
		return selectedLLMProvider{}, errors.New("selected cliproxy provider configuration is incomplete")
	}

	return selectedLLMProvider{
		name:           settings.LLMProviderCliproxy,
		baseURL:        config.BaseURL,
		apiKey:         config.APIKey,
		defaultModel:   config.Model,
		reasoningLevel: config.ReasoningEffort,
	}, nil
}

func logProviderInitializationError(logger *slog.Logger, name string, err error) {
	if errors.Is(err, errSelectedLLMProviderDisabled) {
		logger.Info(name + " disabled")

		return
	}

	logger.Error(name+" initialization failed", slog.Any("error", err))
}

func normalizedProviderName(name string) string {
	return cmp.Or(strings.ToLower(strings.TrimSpace(name)), settings.LLMProviderCliproxy)
}

func selectedProviderReasoningLevel(config settings.LLMProviderConfig) string {
	if normalizedProviderName(config.Name) == settings.LLMProviderGemini {
		return config.Gemini.ThinkingLevel
	}

	return config.Cliproxy.ReasoningEffort
}

func appendSupportedTemperature(opts []llm.Option, model string, temperature float64) ([]llm.Option, bool) {
	if temperature <= 0 || strings.Contains(strings.ToLower(strings.TrimSpace(model)), "gemini-3.7-flash") {
		return opts, false
	}

	return append(opts, llm.WithTemperature(temperature)), true
}
