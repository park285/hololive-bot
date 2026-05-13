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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

var _ Client = (*OpenAIClient)(nil)
var errOpenAIEmptyOutput = errors.New("openai: 응답에 텍스트 출력 없음")

type OpenAIClient struct {
	client          openai.Client
	model           string
	schemaName      string
	temperature     *float64
	reasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
	webSearch       bool
	chatCompletions bool // true=Chat Completions API (cliproxy 호환)
	logger          *slog.Logger
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
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	)

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

	return &OpenAIClient{
		client:          client,
		model:           model,
		schemaName:      o.SchemaName,
		temperature:     o.Temperature,
		reasoningEffort: o.ReasoningEffort,
		webSearch:       webSearch,
		chatCompletions: o.ChatCompletions,
		logger:          logger,
	}
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

	if c.chatCompletions {
		return c.generateJSONChatCompletions(ctx, systemPrompt, userPrompt, schema)
	}

	text, usedFallback, err := c.generateJSONWithTransportFallback(ctx, systemPrompt, userPrompt, schema)
	if err != nil {
		return "", err
	}

	text, err = c.applyFallbackPostProcess(text, usedFallback)
	if err != nil {
		return "", err
	}

	return text, nil
}

func (c *OpenAIClient) generateJSONWithTransportFallback(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, bool, error) {
	// 기본 경로: Responses API (web_search/tooling 지원)
	text, err := c.generateJSONResponses(ctx, systemPrompt, userPrompt, schema)
	if err == nil {
		return text, false, nil
	}

	if !shouldFallbackToChat(err) {
		return "", false, err
	}

	if ctx != nil && ctx.Err() != nil {
		if c.logger != nil {
			c.logger.Warn("responses API failed and context already done; skipping fallback",
				slog.String("error", err.Error()),
				slog.String("context_error", ctx.Err().Error()),
				slog.String("model", c.model))
		}
		return "", false, err
	}

	// 조건부 fallback: Responses API 미지원/일시 오류 시 Chat Completions 재시도
	if c.logger != nil {
		c.logger.Warn("responses API failed; conditional fallback to chat completions",
			slog.String("error", err.Error()),
			slog.String("model", c.model))
	}

	fallbackText, fallbackErr := c.generateJSONChatCompletions(ctx, systemPrompt, userPrompt, schema)
	if fallbackErr != nil {
		return "", true, fmt.Errorf("responses failed (%w) and fallback failed: %w", err, fallbackErr)
	}

	return fallbackText, true, nil
}

func (c *OpenAIClient) applyFallbackPostProcess(text string, usedFallback bool) (string, error) {
	if !usedFallback {
		return text, nil
	}
	if c.schemaName != "event_summary" {
		return text, nil
	}

	sanitized, err := suppressDiscoveredEvents(text)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to sanitize discovered_events on fallback",
				slog.String("error", err.Error()))
		}
		return text, nil
	}

	return sanitized, nil
}

// generateJSONResponses: Responses API + JSON Schema로 구조화 출력을 생성합니다.
func (c *OpenAIClient) generateJSONResponses(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	params := responses.ResponseNewParams{
		Model:        c.model,
		Instructions: openai.String(systemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema(c.schemaName, schema),
		},
	}
	if c.webSearch {
		params.Tools = []responses.ToolUnionParam{
			responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch),
		}
	}
	if c.temperature != nil {
		params.Temperature = openai.Float(*c.temperature)
	}
	if c.reasoningEffort != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort: shared.ReasoningEffort(c.reasoningEffort),
		}
	}

	resp, err := c.client.Responses.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai responses API: %w", err)
	}

	return extractResponsesOutputText(resp)
}

func extractResponsesOutputText(resp *responses.Response) (string, error) {
	return extractResponsesOutputTextWithDiagnostics(resp)
}

// generateJSONChatCompletions: Chat Completions API로 구조화 JSON 출력을 생성합니다.
// system prompt에 JSON schema를 삽입하고, 응답에서 JSON을 추출합니다.
func (c *OpenAIClient) generateJSONChatCompletions(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("marshal schema: %w", err)
	}

	// system prompt에 JSON schema 지시 삽입 (cliproxy Structured() 패턴)
	jsonSystemPrompt := fmt.Sprintf(`%s

IMPORTANT: You MUST respond with ONLY a valid JSON object that follows this schema (no markdown, no explanation, just the JSON):
%s

Do not include any text before or after the JSON. Only output the JSON object.`, systemPrompt, string(schemaJSON))

	params := openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(jsonSystemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	if c.temperature != nil {
		params.Temperature = openai.Float(*c.temperature)
	}
	if c.reasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(c.reasoningEffort)
	}

	completion, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai chat completions API: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("openai: chat completions 응답에 choices 없음")
	}

	text := completion.Choices[0].Message.Content
	// 마크다운 펜스 제거 + JSON 추출
	extracted, err := jsonutil.Extract(text)
	if err != nil {
		return "", fmt.Errorf("chat completions JSON 추출 실패: %w raw=%q", err, truncateForError(text, 2048))
	}

	return string(extracted), nil
}

func truncateForError(value string, maxBytes int) string {
	return truncateForOpenAIError(value, maxBytes)
}

func shouldFallbackToChat(err error) bool {
	return shouldFallbackToChatCompletions(err)
}

func suppressDiscoveredEvents(rawJSON string) (string, error) {
	return suppressFallbackDiscoveredEvents(rawJSON)
}
