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
	"bytes"
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
)

const maxGeminiResponseBytes = 4 << 20

var _ Client = (*GeminiClient)(nil)

var errGeminiEmptyOutput = errors.New("gemini: 응답에 텍스트 출력 없음")

type GeminiClient struct {
	endpoint      string
	apiKey        string
	model         string
	thinkingLevel string
	webSearch     bool
	httpClient    *http.Client
	logger        *slog.Logger
	costTracker   CostTracker
}

type geminiInteractionRequest struct {
	Model             string                 `json:"model"`
	Input             string                 `json:"input"`
	SystemInstruction string                 `json:"system_instruction,omitempty"`
	Store             bool                   `json:"store"`
	Tools             []geminiTool           `json:"tools,omitempty"`
	ResponseFormat    geminiResponseFormat   `json:"response_format"`
	GenerationConfig  geminiGenerationConfig `json:"generation_config"`
}

type geminiTool struct {
	Type string `json:"type"`
}

type geminiResponseFormat struct {
	Type     string         `json:"type"`
	MIMEType string         `json:"mime_type"`
	Schema   map[string]any `json:"schema"`
}

type geminiGenerationConfig struct {
	ThinkingLevel string `json:"thinking_level"`
}

type geminiInteractionResponse struct {
	Status string       `json:"status"`
	Steps  []geminiStep `json:"steps"`
	Usage  geminiUsage  `json:"usage"`
}

type geminiStep struct {
	Type    string          `json:"type"`
	Content []geminiContent `json:"content"`
}

type geminiContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type geminiUsage struct {
	TotalTokens int64 `json:"total_tokens"`
}

type geminiErrorEnvelope struct {
	Error geminiErrorBody `json:"error"`
}

type geminiErrorBody struct {
	Code   int    `json:"code"`
	Status string `json:"status"`
}

type geminiProviderError struct {
	statusCode int
	code       string
}

func (e geminiProviderError) Error() string {
	return safeProviderError{statusCode: e.statusCode, code: e.code, apiType: "gemini"}.Error()
}

func NewGeminiClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) (*GeminiClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)

	endpoint, err := geminiInteractionsEndpoint(baseURL, apiKey, model)
	if err != nil {
		return nil, err
	}

	o := &Options{SchemaName: "event_summary"}
	for _, opt := range opts {
		opt(o)
	}

	thinkingLevel, err := normalizeGeminiThinkingLevel(o.ReasoningEffort)
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &GeminiClient{
		endpoint:      endpoint,
		apiKey:        apiKey,
		model:         model,
		thinkingLevel: thinkingLevel,
		webSearch:     resolveWebSearch(o),
		httpClient:    newLLMHTTPClient(),
		logger:        logger,
		costTracker:   o.CostTracker,
	}, nil
}

func geminiInteractionsEndpoint(baseURL, apiKey, model string) (string, error) {
	if err := validateGeminiClientInputs(apiKey, model); err != nil {
		return "", err
	}

	parsedURL, err := parseGeminiBaseURL(baseURL)
	if err != nil {
		return "", err
	}

	return parsedURL.JoinPath("v1beta", "interactions").String(), nil
}

func validateGeminiClientInputs(apiKey, model string) error {
	if apiKey == "" {
		return errors.New("gemini API key is empty")
	}

	if model == "" {
		return errors.New("gemini model is empty")
	}

	return nil
}

func parseGeminiBaseURL(baseURL string) (*url.URL, error) {
	if baseURL == "" {
		return nil, errors.New("gemini base URL is empty")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, errors.New("gemini base URL is invalid")
	}

	if err := validateGeminiBaseURL(parsedURL); err != nil {
		return nil, err
	}

	return parsedURL, nil
}

func validateGeminiBaseURL(endpoint *url.URL) error {
	if endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("gemini base URL is invalid")
	}

	if endpoint.Scheme != "https" && !isLoopbackHTTP(endpoint) {
		return errors.New("gemini base URL must use HTTPS")
	}

	return nil
}

func isLoopbackHTTP(endpoint *url.URL) bool {
	if endpoint.Scheme != "http" {
		return false
	}

	host := endpoint.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}

	address, err := netip.ParseAddr(host)

	return err == nil && address.IsLoopback()
}

func normalizeGeminiThinkingLevel(level string) (string, error) {
	level = cmp.Or(strings.ToLower(strings.TrimSpace(level)), "high")

	switch level {
	case "low", "medium", "high":
		return level, nil
	default:
		return "", errors.New("gemini thinking level must be one of low, medium, high")
	}
}

func (c *GeminiClient) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c == nil {
		return "", errors.New("gemini client is nil")
	}

	if ctx == nil {
		return "", errors.New("gemini context is nil")
	}

	if schema == nil {
		return "", errors.New("json schema is nil")
	}

	attrs := llmPromptSummaryAttrs("gemini", c.model, systemPrompt, userPrompt)
	sharedlog.Debug(ctx, c.logger, "llm.prompt.built", "llm prompt built", attrs...)
	sharedlog.Info(ctx, c.logger, "llm.provider.request.started", "llm provider request started", attrs...)

	started := time.Now()
	text, usage, err := c.generate(ctx, systemPrompt, userPrompt, schema)
	if err != nil {
		failedAttrs := append([]slog.Attr{}, attrs...)
		failedAttrs = append(failedAttrs, sharedlog.SinceMS(started))
		failedAttrs = append(failedAttrs, llmProviderErrorAttrs(err)...)
		sharedlog.Error(ctx, c.logger, "llm.provider.request.failed", "llm provider request failed", failedAttrs...)

		return "", fmt.Errorf("generate JSON: %w", safeLLMProviderError(err))
	}

	if c.costTracker != nil && usage > 0 {
		c.costTracker.RecordUsage(ctx, "gemini", c.model, usage)
	}

	successAttrs := append([]slog.Attr{}, attrs...)
	successAttrs = append(successAttrs, sharedlog.SinceMS(started), slog.Int("result_count", 1))
	sharedlog.Info(ctx, c.logger, "llm.provider.request.succeeded", "llm provider request succeeded", successAttrs...)
	sharedlog.Debug(ctx, c.logger, "llm.result.validated", "llm result validated", successAttrs...)

	return text, nil
}

func (c *GeminiClient) generate(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, int64, error) {
	req, err := c.newInteractionRequest(ctx, systemPrompt, userPrompt, schema)
	if err != nil {
		return "", 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("send gemini interaction request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := readGeminiResponse(resp.Body)
	if err != nil {
		return "", 0, err
	}

	return decodeGeminiInteraction(raw, resp.StatusCode)
}

func (c *GeminiClient) newInteractionRequest(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (*http.Request, error) {
	reqBody := geminiInteractionRequest{
		Model:             c.model,
		Input:             userPrompt,
		SystemInstruction: systemPrompt,
		Store:             false,
		ResponseFormat: geminiResponseFormat{
			Type:     "text",
			MIMEType: "application/json",
			Schema:   schema,
		},
		GenerationConfig: geminiGenerationConfig{ThinkingLevel: c.thinkingLevel},
	}
	if c.webSearch {
		reqBody.Tools = []geminiTool{{Type: "google_search"}}
	}

	var body bytes.Buffer
	if err := jsonv2.MarshalWrite(&body, reqBody); err != nil {
		return nil, fmt.Errorf("marshal gemini interaction: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("create gemini interaction request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	return req, nil
}

func readGeminiResponse(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxGeminiResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read gemini interaction response: %w", err)
	}

	if len(raw) > maxGeminiResponseBytes {
		return nil, errors.New("gemini interaction response exceeds size limit")
	}

	return raw, nil
}

func decodeGeminiInteraction(raw []byte, statusCode int) (string, int64, error) {
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return "", 0, newGeminiProviderError(raw, statusCode)
	}

	var interaction geminiInteractionResponse
	if err := jsonv2.Unmarshal(raw, &interaction); err != nil {
		return "", 0, fmt.Errorf("decode gemini interaction response: %w", err)
	}

	if interaction.Status != "completed" {
		return "", 0, safeProviderError{apiType: "gemini", code: interaction.Status, errType: "interaction_not_completed"}
	}

	text := finalGeminiModelOutput(interaction.Steps)
	if text == "" {
		return "", 0, errGeminiEmptyOutput
	}

	var structuredOutput any
	if err := jsonv2.Unmarshal([]byte(text), &structuredOutput); err != nil {
		return "", 0, errors.New("gemini interaction returned invalid JSON output")
	}

	return text, interaction.Usage.TotalTokens, nil
}

func newGeminiProviderError(raw []byte, statusCode int) geminiProviderError {
	providerErr := geminiProviderError{statusCode: statusCode}
	var envelope geminiErrorEnvelope
	if jsonv2.Unmarshal(raw, &envelope) == nil {
		providerErr.code = strings.TrimSpace(envelope.Error.Status)
	}

	return providerErr
}

func finalGeminiModelOutput(steps []geminiStep) string {
	for _, step := range slices.Backward(steps) {
		if step.Type != "model_output" {
			continue
		}

		var output strings.Builder
		for _, content := range step.Content {
			if content.Type == "text" {
				output.WriteString(content.Text)
			}
		}

		return strings.TrimSpace(output.String())
	}

	return ""
}
