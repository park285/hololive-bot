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

import "context"

type Client interface {
	GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error)
}

type Options struct {
	SchemaName      string
	Temperature     *float64
	WebSearch       *bool  // nil=기본값(true), false=비활성화
	ChatCompletions bool   // true=Chat Completions API 사용 (cliproxy 호환)
	ReasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
	CostTracker     CostTracker
}

type Option func(*Options)

func WithSchemaName(name string) Option {
	return func(o *Options) {
		if name != "" {
			o.SchemaName = name
		}
	}
}

func WithTemperature(temp float64) Option {
	return func(o *Options) {
		if temp > 0 {
			o.Temperature = &temp
		}
	}
}

func WithWebSearch(enabled bool) Option {
	return func(o *Options) {
		o.WebSearch = &enabled
	}
}

func WithChatCompletions() Option {
	return func(o *Options) {
		o.ChatCompletions = true
	}
}

func WithReasoningEffort(effort string) Option {
	return func(o *Options) {
		if effort != "" {
			o.ReasoningEffort = effort
		}
	}
}

// WithCostTracker는 토큰 사용량 관측기를 주입한다. nil이면 관측 비활성(no-op).
func WithCostTracker(tracker CostTracker) Option {
	return func(o *Options) {
		if tracker != nil {
			o.CostTracker = tracker
		}
	}
}
