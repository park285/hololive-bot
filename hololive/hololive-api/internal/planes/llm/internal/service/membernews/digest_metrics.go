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

package membernews

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
)

// digest fallback 사유는 bounded enum이다. 원문 오류나 room 식별자를 label에 넣지 않는다.
const (
	digestFallbackReasonNone            = ""
	digestFallbackReasonLLMDisabled     = "llm_disabled"
	digestFallbackReasonValidationEmpty = "validation_empty"
	digestFallbackReasonSummarizerError = "summarizer_error"
	digestFallbackReasonEmptyResult     = "empty_result"
)

var (
	digestMetricsOnce       sync.Once
	digestResultTotal       *prometheus.CounterVec
	digestResultReasonPairs = []struct {
		resultType sharedmodel.SummaryResultType
		reason     string
	}{
		{sharedmodel.SummaryResultPrimary, digestFallbackReasonNone},
		{sharedmodel.SummaryResultEmpty, digestFallbackReasonNone},
		{sharedmodel.SummaryResultFallback, digestFallbackReasonLLMDisabled},
		{sharedmodel.SummaryResultFallback, digestFallbackReasonValidationEmpty},
		{sharedmodel.SummaryResultFallback, digestFallbackReasonSummarizerError},
		{sharedmodel.SummaryResultFallback, digestFallbackReasonEmptyResult},
	}
)

func initDigestMetrics() {
	digestMetricsOnce.Do(func() {
		digestResultTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_member_news_digest_result_total",
				Help: "Member news room digests by result type and deterministic fallback reason.",
			},
			[]string{"result_type", "reason"},
		)

		for _, pair := range digestResultReasonPairs {
			digestResultTotal.WithLabelValues(string(pair.resultType), pair.reason)
		}
	})
}

func observeDigestResult(resultType sharedmodel.SummaryResultType, reason string) {
	initDigestMetrics()
	digestResultTotal.WithLabelValues(string(resultType), reason).Inc()
}
