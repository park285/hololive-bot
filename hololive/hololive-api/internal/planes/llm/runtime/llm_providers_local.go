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
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

var errSelectedLLMProviderDisabled = errors.New("selected LLM provider is disabled")

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
	incompleteWarn string
	successMessage string
	preset         bool
}

func ProvideLLMCostTracker(cacheClient cache.Client, monthlyCeiling int64, logger *slog.Logger) llm.CostTracker {
	if tracker := llm.NewValkeyCostCeiling(cacheClient, monthlyCeiling, logger); tracker != nil {
		return tracker
	}

	return nil
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

func ProvideMemberNewsLLMClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
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
	if err != nil {
		logProviderInitializationError(logger, "Member news LLM", err)

		return nil
	}

	logger.Info("Member news LLM enabled",
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.String("model", model),
		slog.Bool("temperature_applied", temperatureApplied),
		slog.Float64("temperature", llmConfig.MemberNewsTemperature),
	)

	return client
}

func ProvideMemberNewsReviewerClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	model := cmp.Or(llmConfig.MemberNews.ReviewerModel, llmConfig.MemberNewsModel)

	return buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		schemaName:     "member_news_review",
		temperature:    0.1,
		chatCompletion: true,
		incompleteWarn: "Consensus reviewer LLM configuration incomplete, skipping",
		successMessage: "Consensus reviewer LLM enabled",
		preset:         true,
	})
}

func ProvideMajorEventReviewerClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	return buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          llmConfig.MajorEvent.ReviewerModel,
		schemaName:     "event_summary_review",
		incompleteWarn: "Major event consensus reviewer LLM configuration incomplete, skipping",
		successMessage: "Major event consensus reviewer LLM enabled",
		preset:         true,
	})
}

func ProvideMajorEventAdjudicatorClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	return buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MajorEvent.Enabled,
		model:          llmConfig.MajorEvent.AdjudicatorModel,
		schemaName:     "event_summary",
		incompleteWarn: "Major event consensus adjudicator LLM configuration incomplete, skipping",
		successMessage: "Major event consensus adjudicator LLM enabled",
	})
}

func ProvideMemberNewsAdjudicatorClient(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llm.CostTracker, logger *slog.Logger) llm.Client {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	model := cmp.Or(llmConfig.MemberNews.AdjudicatorModel, llmConfig.MemberNewsModel)

	return buildConsensusLLMClient(provider, tracker, logger, consensusClientSpec{
		enabled:        llmConfig.MemberNews.Enabled,
		model:          model,
		schemaName:     "member_news_summary",
		temperature:    llmConfig.MemberNewsTemperature,
		chatCompletion: true,
		incompleteWarn: "Consensus adjudicator LLM configuration incomplete, skipping",
		successMessage: "Consensus adjudicator LLM enabled",
		preset:         true,
	})
}

func buildConsensusLLMClient(provider settings.LLMProviderConfig, tracker llm.CostTracker, logger *slog.Logger, spec consensusClientSpec) llm.Client {
	if !spec.enabled {
		return nil
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
		if errors.Is(err, errSelectedLLMProviderDisabled) {
			return nil
		}

		logger.Warn(spec.incompleteWarn,
			slog.String("provider", normalizedProviderName(provider.Name)),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info(spec.successMessage,
		slog.String("provider", normalizedProviderName(provider.Name)),
		slog.String("model", model),
	)

	return client
}

func buildProviderClient(provider settings.LLMProviderConfig, tracker llm.CostTracker, logger *slog.Logger, spec providerClientSpec) (llm.Client, string, bool, error) {
	selected, err := selectLLMProvider(provider)
	if err != nil {
		return nil, "", false, err
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
		return buildGeminiProviderClient(selected, model, logger, opts)
	}

	return buildCliproxyProviderClient(selected, model, logger, opts, spec)
}

func buildGeminiProviderClient(selected selectedLLMProvider, model string, logger *slog.Logger, opts []llm.Option) (llm.Client, string, bool, error) {
	client, err := llm.NewGeminiClient(selected.baseURL, selected.apiKey, model, logger, opts...)
	if err != nil {
		return nil, "", false, fmt.Errorf("initialize gemini LLM client: %w", err)
	}

	return client, model, false, nil
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
		return selectGeminiProvider(config.Gemini)
	case settings.LLMProviderCliproxy:
		return selectCliproxyProvider(config.Cliproxy)
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
