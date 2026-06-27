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
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeCostTracker struct {
	providers []string
	models    []string
	tokens    []int64
}

func (f *fakeCostTracker) RecordUsage(_ context.Context, provider, model string, tokens int64) {
	f.providers = append(f.providers, provider)
	f.models = append(f.models, model)
	f.tokens = append(f.tokens, tokens)
}

func TestOpenAIClient_recordUsage_DelegatesPositiveTokensWithProviderModel(t *testing.T) {
	ft := &fakeCostTracker{}
	c := NewClient("https://example.com", "key", "gpt-test", nil, WithCostTracker(ft))

	c.recordUsage(context.Background(), 1234)
	require.Equal(t, []int64{1234}, ft.tokens)
	require.Equal(t, []string{"openai"}, ft.providers)
	require.Equal(t, []string{"gpt-test"}, ft.models)
}

func TestOpenAIClient_recordUsage_SkipsZeroOrNegativeTokens(t *testing.T) {
	ft := &fakeCostTracker{}
	c := NewClient("https://example.com", "key", "gpt-test", nil, WithCostTracker(ft))

	c.recordUsage(context.Background(), 0)
	c.recordUsage(context.Background(), -5)
	require.Empty(t, ft.tokens)
}

func TestOpenAIClient_recordUsage_NoTrackerIsNoOp(t *testing.T) {
	c := NewClient("https://example.com", "key", "gpt-test", nil)
	require.NotPanics(t, func() {
		c.recordUsage(context.Background(), 100)
	})

	// WithCostTracker(nil)은 tracker를 설정하지 않아야 한다(no-op).
	c2 := NewClient("https://example.com", "key", "gpt-test", nil, WithCostTracker(nil))
	require.Nil(t, c2.costTracker)
}
