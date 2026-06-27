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
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	costCeilingKeyPrefix = "llm:cost:tokens:"
	// 월 카운터는 다음 달 집계와 디버깅 여지를 위해 70일 보존 후 자동 만료.
	costCeilingKeyTTL = 70 * 24 * time.Hour
)

var (
	costMetricsOnce      sync.Once
	llmCostTokensTotal   *prometheus.CounterVec
	llmCostCeilingExceed prometheus.Counter
)

func initCostMetrics() {
	costMetricsOnce.Do(func() {
		llmCostTokensTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_llm_cost_tokens_total",
				Help: "Total LLM provider tokens consumed by provider.",
			},
			[]string{"provider"},
		)
		llmCostCeilingExceed = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_llm_cost_ceiling_exceeded_total",
				Help: "Number of times the monthly LLM token ceiling was crossed.",
			},
		)
	})
}

// ValkeyCostCeiling은 6개 provider 인스턴스가 공유하는 월 단위 토큰 카운터를 Valkey에 누적한다.
// 임계(monthlyCeiling) 초과 시 경고만 남기고 호출은 차단하지 않는다(초기 정책: 관측 우선).
// RecordUsage는 LLM 응답 핫패스에서 호출되므로 에러를 전파하지 않고 로깅 후 계속한다.
type ValkeyCostCeiling struct {
	cache          cache.Client
	monthlyCeiling int64
	logger         *slog.Logger
	now            func() time.Time
}

var _ CostTracker = (*ValkeyCostCeiling)(nil)

// NewValkeyCostCeiling은 cache가 nil이거나 ceiling<=0이면 nil을 반환해 관측을 비활성한다
// (호출부는 WithCostTracker(nil)로 안전하게 no-op 처리).
func NewValkeyCostCeiling(cacheClient cache.Client, monthlyCeiling int64, logger *slog.Logger) *ValkeyCostCeiling {
	if cacheClient == nil || monthlyCeiling <= 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	initCostMetrics()
	return &ValkeyCostCeiling{
		cache:          cacheClient,
		monthlyCeiling: monthlyCeiling,
		logger:         logger,
		now:            time.Now,
	}
}

func (v *ValkeyCostCeiling) monthKey(t time.Time) string {
	return costCeilingKeyPrefix + t.UTC().Format("2006-01")
}

func (v *ValkeyCostCeiling) RecordUsage(ctx context.Context, provider, model string, tokens int64) {
	if v == nil || tokens <= 0 {
		return
	}
	if llmCostTokensTotal != nil {
		llmCostTokensTotal.WithLabelValues(provider).Add(float64(tokens))
	}

	key := v.monthKey(v.now())
	builder := v.cache.B()
	results := v.cache.DoMulti(ctx,
		builder.Incrby().Key(key).Increment(tokens).Build(),
		builder.Expire().Key(key).Seconds(int64(costCeilingKeyTTL.Seconds())).Build(),
	)
	if len(results) == 0 {
		return
	}
	total, err := results[0].AsInt64()
	if err != nil {
		v.logger.WarnContext(ctx, "llm cost ceiling: failed to increment monthly token counter",
			slog.String("provider", provider),
			slog.String("error_type", llmErrorType(err)))
		return
	}

	if total > v.monthlyCeiling && total-tokens <= v.monthlyCeiling {
		v.warnCeilingExceeded(ctx, provider, model, total, key)
	}
}

func (v *ValkeyCostCeiling) warnCeilingExceeded(ctx context.Context, provider, model string, total int64, key string) {
	if llmCostCeilingExceed != nil {
		llmCostCeilingExceed.Inc()
	}
	v.logger.WarnContext(ctx, "llm monthly token ceiling exceeded",
		slog.String("provider", provider),
		slog.String("model", model),
		slog.Int64("month_tokens", total),
		slog.Int64("ceiling", v.monthlyCeiling),
		slog.String("month", key))
}
