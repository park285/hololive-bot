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
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

var _ Client = (*OpenAIClient)(nil)

// OpenAIClient는 OpenAI 호환 provider에 openaipreset의 계층 경로(GenerateJSONAs)로만 요청한다. 단일 system prompt 경로는 없다.
// 오류 원문 제거는 shared-go generator가 맡는다. 여기서 safeLLMProviderError로 다시 분류하면 status_code·code가 사라진다.
type OpenAIClient struct {
	client *openaipreset.Client
	// task는 요청의 task·schema 이름이자 prompt cache key 접미사다.
	task string
}

// NewClient는 schema 이름이 비어 있으면 client를 만들지 않는다.
func NewClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) (*OpenAIClient, error) {
	if logger == nil {
		logger = slog.Default()
	}

	o := &Options{}

	for _, opt := range opts {
		opt(o)
	}

	task := strings.TrimSpace(o.SchemaName)
	if task == "" {
		return nil, errors.New("create openai client: schema name is empty")
	}

	presetOpts := []openaipreset.Option{
		openaipreset.WithHTTPClient(newLLMHTTPClient()),
		openaipreset.WithLogger(logger),
		openaipreset.WithWebSearch(resolveWebSearch(o)),
		openaipreset.WithMaxRetries(llmMaxRetries),
	}

	if o.Temperature != nil {
		presetOpts = append(presetOpts, openaipreset.WithTemperature(*o.Temperature))
	}

	if o.ReasoningEffort != "" {
		presetOpts = append(presetOpts, openaipreset.WithReasoningEffort(o.ReasoningEffort))
	}

	if o.ChatCompletions {
		presetOpts = append(presetOpts, openaipreset.WithChatCompletions())
	}

	if o.CostTracker != nil {
		presetOpts = append(presetOpts, openaipreset.WithUsageReporter(costTrackerUsageReporter{tracker: o.CostTracker}))
	}

	client, err := openaipreset.New(baseURL, apiKey, model, presetOpts...)
	if err != nil {
		return nil, fmt.Errorf("create openai client: %w", err)
	}

	return &OpenAIClient{client: client, task: task}, nil
}

// GenerateJSON은 provider 출력이 JSON 값 하나일 때만 원문을 돌려준다. 호출자의 jsonv2 해석과 같은
// 기준(jsontext.Value 복호)으로 검사하며, 실패 오류에는 provider 출력 원문이 들어가지 않는다.
// ChatCompletions이면 Chat Completions API, 아니면 Responses API를 쓰고, provider별 역할 매핑은 shared-go가 소유한다.
func (c *OpenAIClient) GenerateJSON(ctx context.Context, prompts openaipreset.PromptLayers, schema map[string]any) (string, error) {
	raw, err := c.client.GenerateJSONAs[jsontext.Value](ctx, c.task, prompts, schema)
	if err != nil {
		return "", fmt.Errorf("generate layered JSON: %w", err)
	}

	return string(raw), nil
}

// LLM 호출은 응답이 유실되면 과금과 웹 검색 tool 사용이 중복될 수 있는 비멱등 비용 작업이고 재시도를 정당화할 계약·독립 근거가 없으므로 재시도하지 않는다. 실패는 그대로 호출자에게 드러난다.
const llmMaxRetries = 0

func newLLMHTTPClient() *http.Client {
	return httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:               2 * time.Minute,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // GPT-5.4 요약 요청은 첫 헤더까지 수십 초가 걸릴 수 있다.
		IdleConnTimeout:       90 * time.Second,
	})
}

func resolveWebSearch(o *Options) bool {
	webSearch := true

	if o.WebSearch != nil {
		webSearch = *o.WebSearch
	}

	if o.ChatCompletions {
		webSearch = false
	}

	return webSearch
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

// 로그에는 원문 대신 계층을 순서대로 이은 길이와 해시 앞 8바이트만 남긴다.
func llmPromptSummaryAttrs(provider, model string, prompts openaipreset.PromptLayers) []slog.Attr {
	prompt := strings.TrimSpace(prompts.Invariant + "\n" + prompts.Developer + "\n" + prompts.User)
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
