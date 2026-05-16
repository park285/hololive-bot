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
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeMajorEventLLMClient struct{}

func (fakeMajorEventLLMClient) GenerateJSON(context.Context, string, string, map[string]any) (string, error) {
	return "", nil
}

func searchProviderTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProvideExaSearcher(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		searcher := provideExaSearcher(config.ExaConfig{Enabled: false, APIKey: "key"}, searchProviderTestLogger())
		assert.Nil(t, searcher)
	})

	t.Run("missing api key", func(t *testing.T) {
		t.Parallel()

		searcher := provideExaSearcher(config.ExaConfig{Enabled: true, APIKey: ""}, searchProviderTestLogger())
		assert.Nil(t, searcher)
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()

		searcher := provideExaSearcher(config.ExaConfig{
			Enabled:  true,
			Endpoint: "https://exa.example.com",
			APIKey:   "test-api-key",
		}, searchProviderTestLogger())
		require.NotNil(t, searcher)
	})
}

func TestProvideEventSummarizer_Default(t *testing.T) {
	t.Parallel()

	summarizer := provideEventSummarizer(
		config.ConsensusLLMConfig{},
		nil,
		nil,
		nil,
		nil,
		nil,
		searchProviderTestLogger(),
	)
	require.NotNil(t, summarizer)

	got := summarizer.Summarize(context.Background(), nil, mesummarizer.SummaryTypeWeekly, "2026-W10")
	assert.Equal(t, "", got)
}

func TestProvideEventSummarizer_ConsensusEnabled(t *testing.T) {
	t.Parallel()

	llm := fakeMajorEventLLMClient{}
	reviewer := fakeMajorEventLLMClient{}
	adjudicator := fakeMajorEventLLMClient{}

	summarizer := provideEventSummarizer(
		config.ConsensusLLMConfig{
			Enabled:           true,
			Confidence:        0.82,
			ReviewTimeout:     7,
			AdjudicateTimeout: 9,
		},
		llm,
		reviewer,
		adjudicator,
		nil,
		nil,
		searchProviderTestLogger(),
	)
	require.NotNil(t, summarizer)

	events := []domain.MajorEvent{}
	got := summarizer.Summarize(context.Background(), events, mesummarizer.SummaryTypeWeekly, "2026-W10")
	assert.Equal(t, "", got)
}
