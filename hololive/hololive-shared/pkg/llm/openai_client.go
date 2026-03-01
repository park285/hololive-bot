package llm

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

var _ Client = (*OpenAIClient)(nil)

// OpenAIClient: OpenAI SDK 기반 LLM 클라이언트 (Responses API / Chat Completions 선택)
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

// NewClient: OpenAI 호환 endpoint를 사용하는 LLM 클라이언트를 생성합니다.
func NewClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) *OpenAIClient {
	// HTTP/2 비활성화: Cloudflare가 Go HTTP/2 fingerprint를 차단하는 문제 방지
	httpClient := &http.Client{
		Timeout: constants.LLMHTTPTimeout.Request,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: constants.LLMHTTPTimeout.Dial,
			}).DialContext,
			TLSHandshakeTimeout:   constants.LLMHTTPTimeout.TLSHandshake,
			ResponseHeaderTimeout: constants.LLMHTTPTimeout.ResponseHeader,
			IdleConnTimeout:       constants.LLMHTTPTimeout.IdleConn,
			TLSNextProto:          make(map[string]func(string, *tls.Conn) http.RoundTripper),
		},
	}
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

// GenerateJSON: 구조화 JSON 출력을 생성합니다.
// chatCompletions=true이면 Chat Completions API, 아니면 Responses API를 사용합니다.
func (c *OpenAIClient) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c.chatCompletions {
		return c.generateJSONChatCompletions(ctx, systemPrompt, userPrompt, schema)
	}

	// 기본 경로: Responses API (web_search/tooling 지원)
	text, err := c.generateJSONResponses(ctx, systemPrompt, userPrompt, schema)
	if err == nil {
		return text, nil
	}

	if !shouldFallbackToChat(err) {
		return "", err
	}

	if ctx != nil && ctx.Err() != nil {
		if c.logger != nil {
			c.logger.Warn("responses API failed and context already done; skipping fallback",
				slog.String("error", err.Error()),
				slog.String("context_error", ctx.Err().Error()),
				slog.String("model", c.model))
		}
		return "", err
	}

	// 조건부 fallback: Responses API 미지원/일시 오류 시 Chat Completions 재시도
	if c.logger != nil {
		c.logger.Warn("responses API failed; conditional fallback to chat completions",
			slog.String("error", err.Error()),
			slog.String("model", c.model))
	}

	fallbackText, fallbackErr := c.generateJSONChatCompletions(ctx, systemPrompt, userPrompt, schema)
	if fallbackErr != nil {
		return "", fmt.Errorf("responses failed (%w) and fallback failed: %w", err, fallbackErr)
	}

	// major-event 요약의 경우 fallback 경로에서는 discovered_events를 비활성화하여
	// 근거 없는 추가 발견 항목 생성 가능성을 줄입니다.
	if c.schemaName == "event_summary" {
		sanitized, sanitizeErr := suppressDiscoveredEvents(fallbackText)
		if sanitizeErr != nil {
			if c.logger != nil {
				c.logger.Warn("failed to sanitize discovered_events on fallback",
					slog.String("error", sanitizeErr.Error()))
			}
		} else {
			fallbackText = sanitized
		}
	}

	return fallbackText, nil
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

	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("openai: 응답에 텍스트 출력 없음")
	}

	return text, nil
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
		return "", fmt.Errorf("chat completions JSON 추출 실패: %w (raw: %s)", err, text)
	}

	return string(extracted), nil
}

func shouldFallbackToChat(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "context canceled") {
		return false
	}

	// 404/405는 Responses 미지원과 구분이 필요하므로, responses 경로 맥락이 있을 때만 허용한다.
	if (strings.Contains(msg, "404") || strings.Contains(msg, "405")) &&
		(strings.Contains(msg, "/responses") || strings.Contains(msg, "responses api") || strings.Contains(msg, "responses")) {
		return true
	}

	// 조건부 허용:
	// - responses endpoint 미지원/일시 오류
	// - 네트워크 계층의 일시 장애
	fallbackHints := []string{
		"500",
		"502",
		"503",
		"504",
		"connection refused",
		"timeout",
		"unsupported",
		"not implemented",
		"bad gateway",
	}
	for _, hint := range fallbackHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func suppressDiscoveredEvents(rawJSON string) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		return "", fmt.Errorf("parse fallback json: %w", err)
	}

	if _, exists := payload["discovered_events"]; !exists {
		return rawJSON, nil
	}

	payload["discovered_events"] = make([]any, 0)

	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal sanitized json: %w", err)
	}
	return string(b), nil
}
