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
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	tokenMetricsOnce   sync.Once
	llmCostTokensTotal *prometheus.CounterVec
)

// hololive_llm_cost_tokens_total 이름과 provider 라벨은 대시보드 연속성 계약이라 바꾸지 않는다
// (DEC-20260926-hololive-llm-token-ceiling-retirement). 여러 프로세스의 합산은 Prometheus가 수집 단계에서 한다.
func initTokenMetrics() {
	tokenMetricsOnce.Do(func() {
		llmCostTokensTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_llm_cost_tokens_total",
				Help: "Total LLM provider tokens consumed by provider.",
			},
			[]string{"provider"},
		)
	})
}

// TokenMetricsRecorder는 provider 응답의 토큰 사용량을 Prometheus counter로만 기록한다.
// 월 상한·공유 저장소 카운터·차단은 두지 않는다(DEC-20260926-hololive-llm-token-ceiling-retirement).
type TokenMetricsRecorder struct{}

var _ CostTracker = TokenMetricsRecorder{}

func NewTokenMetricsRecorder() TokenMetricsRecorder {
	initTokenMetrics()

	return TokenMetricsRecorder{}
}

func (TokenMetricsRecorder) RecordUsage(_ context.Context, provider, _ string, tokens int64) {
	if tokens <= 0 {
		return
	}

	initTokenMetrics()
	llmCostTokensTotal.WithLabelValues(provider).Add(float64(tokens))
}
