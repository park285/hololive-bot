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

package youtubedispatch

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
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

	outboxDeliveryRetryAfterClampedTotal prometheus.Counter
	youtubeOutboxV3HandoffTotal          *prometheus.CounterVec
	outboxLiveCatchupSuppressionTotal    *prometheus.CounterVec

	youtubeDeliveryTransitionTotal      *prometheus.CounterVec
	youtubeDeliveryRuleTotal            *prometheus.CounterVec
	youtubeDeliveryLogicalGroupTotal    *prometheus.CounterVec
	youtubeDeliveryOutcomeUnknownTotal  *prometheus.CounterVec
	youtubeDeliveryCommitAdjudication   *prometheus.CounterVec
	youtubeDeliveryAtomicityBreachTotal *prometheus.CounterVec
	youtubeDeliveryLedgerOperationTotal *prometheus.CounterVec
	youtubeDeliveryCleanupGuardTotal    *prometheus.CounterVec
	youtubeOutboxFanoutTotal            *prometheus.CounterVec
	youtubeOutboxAggregateProjection    *prometheus.CounterVec
	youtubeOutboxAggregateLag           prometheus.Histogram
)

const (
	metricLabelResult                         = "result"
	liveCatchupSuppressionResultSuppressed    = "suppressed"
	liveCatchupSuppressionResultCacheError    = "cache_error"
	liveCatchupSuppressionResultInvalidMarker = "invalid_marker"
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
		[]string{metricLabelResult},
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
		[]string{metricLabelResult},
	)

	initOutboxDispatchHistograms()

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

	outboxDeliveryRetryAfterClampedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_delivery_retry_after_clamped_total",
			Help: "Total YouTube outbox delivery HTTP Retry-After hints clamped to the maximum bound.",
		},
	)
	youtubeOutboxV3HandoffTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_v3_handoff_total",
			Help: "YouTube outbox delivery rows handed to the v3 ledger by mode and result.",
		},
		[]string{"mode", metricLabelResult},
	)
	outboxLiveCatchupSuppressionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_live_catchup_suppression_total",
			Help: "YouTube outbox live catch-up suppression marker lookups by result (suppressed, cache_error, invalid_marker).",
		},
		[]string{metricLabelResult},
	)

	initOutboxLifecycleMetrics()
}

func initOutboxLifecycleMetrics() {
	youtubeDeliveryTransitionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_transition_total",
		Help: "Version-fenced YouTube delivery lifecycle operations by operation and durable outcome.",
	}, []string{"operation", metricLabelResult})
	youtubeDeliveryRuleTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_rule_total",
		Help: "Applied YouTube delivery lifecycle policy decisions by stable rule ID.",
	}, []string{"rule", metricLabelResult})
	youtubeDeliveryLogicalGroupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_logical_group_total",
		Help: "Ledger-first YouTube logical delivery group resolutions.",
	}, []string{"resolution", metricLabelResult})
	youtubeDeliveryOutcomeUnknownTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_outcome_unknown_total",
		Help: "YouTube provider effects whose delivery outcome must remain unknown.",
	}, []string{"transport"})
	youtubeDeliveryCommitAdjudication = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_commit_adjudication_total",
		Help: "Primary read-back results after an ambiguous lifecycle commit response.",
	}, []string{"operation", metricLabelResult})
	youtubeDeliveryAtomicityBreachTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_atomicity_breach_total",
		Help: "Detected mixed lifecycle command envelopes by bounded operation name.",
	}, []string{"operation"})
	youtubeDeliveryLedgerOperationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_ledger_operation_total",
		Help: "Logical delivery ledger operations by operation and durable result.",
	}, []string{"operation", metricLabelResult})
	youtubeDeliveryCleanupGuardTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_delivery_cleanup_guard_total",
		Help: "Terminal outbox cleanup decisions by bounded guard reason and result.",
	}, []string{"reason", metricLabelResult})
	youtubeOutboxFanoutTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_outbox_fanout_total",
		Help: "Canonical YouTube outbox fanout materialization outcomes.",
	}, []string{metricLabelResult})
	youtubeOutboxAggregateProjection = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_youtube_outbox_aggregate_projection_total",
		Help: "YouTube outbox aggregate projection outcomes.",
	}, []string{metricLabelResult})
	youtubeOutboxAggregateLag = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hololive_youtube_outbox_aggregate_lag_seconds",
		Help:    "Observed delay from a terminal envelope projection attempt to aggregate convergence.",
		Buckets: prometheus.DefBuckets,
	})
}

func observeLiveCatchupSuppression(result string) {
	initOutboxMetrics()

	if outboxLiveCatchupSuppressionTotal == nil {
		return
	}

	outboxLiveCatchupSuppressionTotal.WithLabelValues(result).Inc()
}

func observeYouTubeOutboxHandoff(mode handoff.Mode, result string, rows int) {
	initOutboxMetrics()

	if youtubeOutboxV3HandoffTotal == nil || rows <= 0 {
		return
	}

	youtubeOutboxV3HandoffTotal.WithLabelValues(string(mode), result).Add(float64(rows))
}

func initOutboxDispatchHistograms() {
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

func observeDeliveryRetryAfterClamped() {
	initOutboxMetrics()

	if outboxDeliveryRetryAfterClampedTotal == nil {
		return
	}

	outboxDeliveryRetryAfterClampedTotal.Inc()
}

func observeOutboxEnqueueOutboxes(result string, n int) {
	initOutboxMetrics()

	if outboxEnqueueOutboxesTotal == nil || n <= 0 {
		return
	}

	outboxEnqueueOutboxesTotal.WithLabelValues(result).Add(float64(n))
}

func observeOutboxEnqueueTargetRooms(n int) {
	initOutboxMetrics()

	if outboxEnqueueTargetRoomsTotal == nil || n <= 0 {
		return
	}

	outboxEnqueueTargetRoomsTotal.Add(float64(n))
}

func observeOutboxDispatchDuration(duration time.Duration) {
	initOutboxMetrics()

	if outboxDispatchDuration == nil {
		return
	}

	outboxDispatchDuration.Observe(duration.Seconds())
}

func observeOutboxDeliveryClaimed(n int) {
	initOutboxMetrics()

	if outboxDeliveryClaimedTotal == nil || n <= 0 {
		return
	}

	outboxDeliveryClaimedTotal.Add(float64(n))
}

func observeOutboxDispatchBatchSize(n int) {
	initOutboxMetrics()

	if outboxDispatchBatchSize == nil || n <= 0 {
		return
	}

	outboxDispatchBatchSize.Observe(float64(n))
}

func observeOutboxDeliveryProcessed(result string, n int) {
	initOutboxMetrics()

	if outboxDeliveryProcessedTotal == nil || n <= 0 {
		return
	}

	outboxDeliveryProcessedTotal.WithLabelValues(result).Add(float64(n))
}

func observeOutboxDispatchTouchedOutboxes(n int) {
	initOutboxMetrics()

	if outboxDispatchTouchedOutboxes == nil || n <= 0 {
		return
	}

	outboxDispatchTouchedOutboxes.Observe(float64(n))
}

func observeLifecycleApply(operation string, result store.ApplyResult, err error, count int) {
	initOutboxMetrics()

	if count <= 0 {
		count = 1
	}

	outcome := result.Outcome.String()
	if result.Outcome == 0 {
		outcome = "error"
	}

	youtubeDeliveryTransitionTotal.WithLabelValues(operation, outcome).Add(float64(count))

	for _, rule := range result.Rules {
		youtubeDeliveryRuleTotal.WithLabelValues(string(rule), outcome).Inc()
	}

	if result.CommitAdjudication != "" {
		youtubeDeliveryCommitAdjudication.WithLabelValues(operation, string(result.CommitAdjudication)).Inc()
	}

	if _, ok := errors.AsType[*store.AtomicityBreachError](err); ok {
		youtubeDeliveryAtomicityBreachTotal.WithLabelValues(operation).Inc()
	}
}

func observeLogicalResolutions(kinds []preparation.ResolutionKind) {
	initOutboxMetrics()

	for _, kind := range kinds {
		result := "resolved"

		if kind == preparation.LogicalInvariantBreach {
			result = "blocked"
		}

		youtubeDeliveryLogicalGroupTotal.WithLabelValues(kind.String(), result).Inc()
	}
}

func observeDeliveryOutcomeUnknown(transport string) {
	initOutboxMetrics()
	youtubeDeliveryOutcomeUnknownTotal.WithLabelValues(transport).Inc()
}

func observeLedgerOperation(operation string, result store.ApplyResult, count int) {
	initOutboxMetrics()

	if count <= 0 {
		return
	}

	youtubeDeliveryLedgerOperationTotal.WithLabelValues(operation, result.Outcome.String()).Add(float64(count))
}

func observeCleanupResult(result store.CleanupResult) {
	initOutboxMetrics()

	if result.DeletedOutboxes > 0 {
		youtubeDeliveryCleanupGuardTotal.WithLabelValues("ledger_backed", "deleted").Add(float64(result.DeletedOutboxes))
	}

	for reason, count := range result.Guards {
		youtubeDeliveryCleanupGuardTotal.WithLabelValues(string(reason), "guarded").Add(float64(count))
	}
}

func observeFanoutResult(result store.FanoutResult, err error) {
	initOutboxMetrics()
	observeLifecycleApply("materialize_fanout", result.ApplyResult, err, 1)
	youtubeOutboxFanoutTotal.WithLabelValues(result.Outcome.String()).Inc()
}

func observeAggregateProjection(result string, lag time.Duration) {
	initOutboxMetrics()
	youtubeOutboxAggregateProjection.WithLabelValues(result).Inc()

	if result == "applied" && youtubeOutboxAggregateLag != nil {
		youtubeOutboxAggregateLag.Observe(lag.Seconds())
	}
}
