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

package delivery

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	outboxMetricsInitOnce sync.Once

	outboxEnqueueOutboxesTotal    *prometheus.CounterVec
	outboxEnqueueTargetRoomsTotal prometheus.Counter
	outboxEnqueueDuration         prometheus.Histogram

	outboxDeliveryClaimedTotal    prometheus.Counter
	outboxDeliveryProcessedTotal  *prometheus.CounterVec
	outboxDispatchDuration        prometheus.Histogram
	outboxDispatchBatchSize       prometheus.Histogram
	outboxDispatchTouchedOutboxes prometheus.Histogram

	outboxRevivedTotal      prometheus.Counter
	outboxReviveErrorsTotal prometheus.Counter
)

func initOutboxMetrics() {
	outboxMetricsInitOnce.Do(func() {
		initOutboxEnqueueMetrics()
		initOutboxDispatchMetrics()
	})
}

func initOutboxEnqueueMetrics() {
	outboxEnqueueOutboxesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_enqueue_outboxes_total",
			Help: "Total YouTube outbox enqueue outcomes by result.",
		},
		[]string{"result"},
	)

	outboxEnqueueTargetRoomsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_enqueue_target_rooms_total",
			Help: "Total target rooms enqueued for YouTube outbox delivery rows.",
		},
	)

	outboxEnqueueDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "hololive_youtube_outbox_enqueue_duration_seconds",
			Help:    "YouTube outbox enqueue batch duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)
}

func initOutboxDispatchMetrics() {
	outboxDeliveryClaimedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_delivery_claimed_total",
			Help: "Total claimed YouTube outbox delivery rows.",
		},
	)

	outboxDeliveryProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_delivery_processed_total",
			Help: "Total processed YouTube outbox delivery rows by result.",
		},
		[]string{"result"},
	)

	outboxDispatchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "hololive_youtube_outbox_dispatch_duration_seconds",
			Help:    "YouTube outbox per-room dispatch duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	outboxDispatchBatchSize = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "hololive_youtube_outbox_dispatch_batch_size",
			Help:    "Claimed YouTube outbox delivery row count per dispatch batch.",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200},
		},
	)

	outboxDispatchTouchedOutboxes = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "hololive_youtube_outbox_dispatch_touched_outboxes",
			Help:    "Unique outbox rows touched per YouTube outbox dispatch batch.",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200},
		},
	)

	outboxRevivedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_revived_total",
			Help: "Total fresh never-sent FAILED YouTube outbox rows revived for redelivery.",
		},
	)

	outboxReviveErrorsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_revive_errors_total",
			Help: "Total stale-failed revival sweep transaction errors.",
		},
	)
}

// observeOutboxRevived는 stale-failed revival sweep이 되살린 outbox 행 수를 누적한다.
// 메트릭 미초기화(NewDispatcher 미경유 단위 테스트 등) 시 no-op.
func observeOutboxRevived(n int64) {
	if outboxRevivedTotal == nil || n <= 0 {
		return
	}
	outboxRevivedTotal.Add(float64(n))
}

// observeOutboxReviveError는 revival sweep 트랜잭션 실패를 누적한다(지속 실패 관측용). 미초기화 시 no-op.
func observeOutboxReviveError() {
	if outboxReviveErrorsTotal == nil {
		return
	}
	outboxReviveErrorsTotal.Inc()
}
