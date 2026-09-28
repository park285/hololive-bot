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
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

var _ Client = (*presetClient)(nil)

// presetClient는 openaipreset의 계층 경로(GenerateJSONAs)로 JSON 원문을 받는다. 단일 system prompt를
// 받던 openaipreset GenerateJSON 경로는 DEC-20260926-stack-llm-instruction-layering-sole-path로 퇴역했다.
type presetClient struct {
	client *openaipreset.Client
	// task는 요청의 task·schema 이름이자 prompt cache key 접미사다.
	task string
}

// GenerateJSON은 provider 출력이 JSON 값 하나일 때만 원문을 돌려준다. 호출자의 jsonv2 해석과 같은
// 기준(jsontext.Value 복호)으로 검사하며, 실패 오류에는 provider 출력 원문이 들어가지 않는다.
func (c *presetClient) GenerateJSON(ctx context.Context, prompts openaipreset.PromptLayers, schema map[string]any) (string, error) {
	raw, err := c.client.GenerateJSONAs[jsontext.Value](ctx, c.task, prompts, schema)
	if err != nil {
		return "", fmt.Errorf("generate layered JSON: %w", err)
	}

	return string(raw), nil
}

func NewPresetClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	o := &Options{}

	for _, opt := range opts {
		opt(o)
	}

	task := strings.TrimSpace(o.SchemaName)
	if task == "" {
		return nil, errors.New("preset client: schema name is empty")
	}

	presetOpts := []openaipreset.Option{
		openaipreset.WithHTTPClient(newLLMHTTPClient()),
		openaipreset.WithLogger(logger),
		openaipreset.WithWebSearch(resolveWebSearch(o)),
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

	out, err := openaipreset.New(baseURL, apiKey, model, presetOpts...)
	if err != nil {
		return nil, fmt.Errorf("preset client: %w", err)
	}

	return &presetClient{client: out, task: task}, nil
}
