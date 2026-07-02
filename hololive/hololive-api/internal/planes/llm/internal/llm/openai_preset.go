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
	"log/slog"

	"github.com/park285/shared-go/pkg/llm/openaipreset"
)

func NewPresetClient(baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	presetOpts := []openaipreset.Option{
		openaipreset.WithHTTPClient(newLLMHTTPClient()),
		openaipreset.WithLogger(logger),
		openaipreset.WithWebSearch(resolveWebSearch(o)),
	}
	if o.SchemaName != "" {
		presetOpts = append(presetOpts, openaipreset.WithSchemaName(o.SchemaName))
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

	return openaipreset.New(baseURL, apiKey, model, presetOpts...)
}
