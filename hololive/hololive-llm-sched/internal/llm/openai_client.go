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

package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	sharedllm "github.com/park285/shared-go/pkg/llm"
	sharedlog "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/constants"
)

var _ Client = (*OpenAIClient)(nil)

var (
	errOpenAIEmptyOutput   = errors.New("openai: 응답에 텍스트 출력 없음")
	errOpenAIRefusalOutput = errors.New("openai: 응답이 안전 정책에 의해 거절됨")
)

type OpenAIClient struct {
	generator       sharedllm.JSONGenerator
	model           string
	schemaName      string
	temperature     *float64
	reasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
	webSearch       bool
	chatCompletions bool // true=Chat Completions API (cliproxy 호환)
	logger          *slog.Logger
	costTracker     CostTracker
}

func NewClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) *OpenAIClient {
	if logger == nil {
		logger = slog.Default()
	}

	// HTTP/2 비활성화: Cloudflare가 Go HTTP/2 fingerprint를 차단하는 문제 방지
	httpClient := httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:               constants.LLMHTTPTimeout.Request,
		DialTimeout:           constants.LLMHTTPTimeout.Dial,
		TLSHandshakeTimeout:   constants.LLMHTTPTimeout.TLSHandshake,
		ResponseHeaderTimeout: constants.LLMHTTPTimeout.ResponseHeader,
		IdleConnTimeout:       constants.LLMHTTPTimeout.IdleConn,
		DisableHTTP2:          true,
	})
	o := &Options{
		SchemaName: "event_summary",
	}
	for _, opt := range opts {
		opt(o)
	}

	webSearch := true // 기본값: 활성화 (majorevent 이벤트 요약 등)
	if o.WebSearch != nil {
		webSearch = *o.WebSearch
	}
	// Chat Completions 모드에서는 Responses API의 web_search tool 미지원
	if o.ChatCompletions {
		webSearch = false
	}

	var generator sharedllm.JSONGenerator
	generator, err := sharedllm.NewOpenAICompatibleJSONGenerator(sharedllm.OpenAICompatibleConfig{
		BaseURL:                      baseURL,
		APIKey:                       apiKey,
		HTTPClient:                   httpClient,
		AllowChatCompletionsFallback: true,
	})
	if err != nil {
		generator = errorJSONGenerator{err: err}
	}

	return &OpenAIClient{
		generator:       generator,
		model:           model,
		schemaName:      o.SchemaName,
		temperature:     o.Temperature,
		reasoningEffort: o.ReasoningEffort,
		webSearch:       webSearch,
		chatCompletions: o.ChatCompletions,
		logger:          logger,
		costTracker:     o.CostTracker,
	}
}

// recordUsage는 provider 응답의 토큰 사용량을 cost tracker에 누적한다. tracker 미주입·토큰 0이면 no-op.
// LLM 응답 경로에서 호출되므로 에러를 전파하지 않는다.
func (c *OpenAIClient) recordUsage(ctx context.Context, tokens int64) {
	if c == nil || c.costTracker == nil || tokens <= 0 {
		return
	}
	c.costTracker.RecordUsage(ctx, "openai", c.model, tokens)
}

type costTrackerUsageReporter struct {
	tracker CostTracker
}

func (r costTrackerUsageReporter) RecordUsage(ctx context.Context, provider, model string, usage sharedllm.Usage) {
	if r.tracker == nil || usage.TotalTokens <= 0 {
		return
	}
	r.tracker.RecordUsage(ctx, provider, model, int64(usage.TotalTokens))
}

// chatCompletions=true이면 Chat Completions API, 아니면 Responses API를 사용합니다.
func (c *OpenAIClient) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c == nil {
		return "", errors.New("openai client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(c.model) == "" {
		return "", errors.New("openai model is empty")
	}
	if schema == nil {
		return "", errors.New("json schema is nil")
	}

	attrs := llmPromptSummaryAttrs("openai", c.model, systemPrompt, userPrompt)
	sharedlog.Debug(ctx, c.logger, "llm.prompt.built", "llm prompt built", attrs...)
	sharedlog.Info(ctx, c.logger, "llm.provider.request.started", "llm provider request started", attrs...)
	started := time.Now()

	resp, err := sharedllm.RunJSON(ctx, c.generator, sharedllm.JSONRequest{
		TaskName:        c.schemaName,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		SchemaName:      c.schemaName,
		Schema:          schema,
		Model:           c.model,
		Temperature:     c.temperature,
		ReasoningEffort: c.reasoningEffort,
		WebSearch:       c.webSearch,
		ChatCompletions: c.chatCompletions,
	}, "openai", costTrackerUsageReporter{tracker: c.costTracker})
	if err != nil {
		safeErr := safeLLMProviderError(err)
		failedAttrs := append([]slog.Attr{}, attrs...)
		failedAttrs = append(failedAttrs, sharedlog.SinceMS(started))
		failedAttrs = append(failedAttrs, llmProviderErrorAttrs(err)...)
		sharedlog.Error(ctx, c.logger, "llm.provider.request.failed", "llm provider request failed", failedAttrs...)
		return "", safeErr
	}

	text, err := c.applyFallbackPostProcess(resp.Text, resp.FallbackUsed)
	if err != nil {
		safeErr := safeLLMProviderError(err)
		failedAttrs := append([]slog.Attr{}, attrs...)
		failedAttrs = append(failedAttrs, sharedlog.SinceMS(started))
		failedAttrs = append(failedAttrs, llmProviderErrorAttrs(err)...)
		sharedlog.Error(ctx, c.logger, "llm.provider.request.failed", "llm provider request failed", failedAttrs...)
		return "", safeErr
	}

	successAttrs := append([]slog.Attr{}, attrs...)
	successAttrs = append(successAttrs, sharedlog.SinceMS(started), slog.Int("result_count", 1))
	sharedlog.Info(ctx, c.logger, "llm.provider.request.succeeded", "llm provider request succeeded", successAttrs...)
	sharedlog.Debug(ctx, c.logger, "llm.result.validated", "llm result validated", successAttrs...)

	return text, nil
}

func (c *OpenAIClient) applyFallbackPostProcess(text string, usedFallback bool) (string, error) {
	if !usedFallback {
		return text, nil
	}
	if c.schemaName != "event_summary" {
		return text, nil
	}

	sanitized, err := suppressFallbackDiscoveredEvents(text)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to sanitize discovered_events on fallback",
				slog.String("error_type", llmErrorType(err)))
		}
		return text, nil
	}

	return sanitized, nil
}

func llmPromptSummaryAttrs(provider, model, systemPrompt, userPrompt string) []slog.Attr {
	prompt := strings.TrimSpace(systemPrompt + "\n" + userPrompt)
	attrs := []slog.Attr{
		slog.String("provider", strings.TrimSpace(provider)),
		slog.String("model", strings.TrimSpace(model)),
		slog.Int("prompt_len", len(prompt)),
	}
	if prompt != "" {
		sum := sha256.Sum256([]byte(prompt))
		attrs = append(attrs, slog.String("prompt_sha256_8", hex.EncodeToString(sum[:8])))
	}
	return attrs
}

type errorJSONGenerator struct {
	err error
}

func (g errorJSONGenerator) GenerateJSON(context.Context, sharedllm.JSONRequest) (sharedllm.JSONResponse, error) {
	return sharedllm.JSONResponse{}, g.err
}
