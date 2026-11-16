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
)

const (
	metricLabelResult                         = "result"
	liveCatchupSuppressionResultSuppressed    = "suppressed"
	liveCatchupSuppressionResultCacheError    = "cache_error"
	liveCatchupSuppressionResultInvalidMarker = "invalid_marker"

	// 묶음(grouped) 전송 fallback 결과다. 개별 요청을 저장해 발송을 시작하면 started이고, 개별 요청을 만들거나 저장하지
	// 못하면 나머지 둘 중 하나다. 이때 그룹은 SENDING에 남아 stale sweeper가 quarantine한다.
	groupedSendFallbackResultStarted       = "started"
	groupedSendFallbackResultPrepareFailed = "prepare_failed"
	groupedSendFallbackResultFreezeFailed  = "freeze_failed"
)

type outboxMetricSet struct {
	enqueueOutboxesTotal    *prometheus.CounterVec
	enqueueTargetRoomsTotal prometheus.Counter
	enqueueDuration         prometheus.Histogram

	deliveryClaimedTotal    prometheus.Counter
	deliveryProcessedTotal  *prometheus.CounterVec
	dispatchDuration        prometheus.Histogram
	dispatchBatchSize       prometheus.Histogram
	dispatchTouchedOutboxes prometheus.Histogram

	revivedTotal      prometheus.Counter
	reviveErrorsTotal prometheus.Counter

	deliveryRetryAfterClampedTotal prometheus.Counter
	liveCatchupSuppressionTotal    *prometheus.CounterVec
	groupedSendFallbackTotal       *prometheus.CounterVec

	deliveryTransitionTotal      *prometheus.CounterVec
	deliveryRuleTotal            *prometheus.CounterVec
	deliveryLogicalGroupTotal    *prometheus.CounterVec
	deliveryOutcomeUnknownTotal  *prometheus.CounterVec
	deliveryCommitAdjudication   *prometheus.CounterVec
	deliveryAtomicityBreachTotal *prometheus.CounterVec
	deliveryLedgerOperationTotal *prometheus.CounterVec
	deliveryCleanupGuardTotal    *prometheus.CounterVec
	outboxFanoutTotal            *prometheus.CounterVec
	outboxAggregateProjection    *prometheus.CounterVec
	outboxAggregateLag           prometheus.Histogram
}

// outboxMetrics는 NewDispatcher와 handoff reconciler 생성자가 먼저 호출해, label 없는 counter·histogram을 기동부터
// 0으로 노출한다.
var outboxMetrics = sync.OnceValue(func() *outboxMetricSet {
	return &outboxMetricSet{
		enqueueOutboxesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_enqueue_outboxes_total",
				Help: "Total YouTube outbox enqueue outcomes by result.",
			},
			[]string{metricLabelResult},
		),
		enqueueTargetRoomsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_enqueue_target_rooms_total",
				Help: "Total target rooms enqueued for YouTube outbox delivery rows.",
			},
		),
		enqueueDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_youtube_outbox_enqueue_duration_seconds",
				Help:    "YouTube outbox enqueue batch duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
		deliveryClaimedTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_delivery_claimed_total",
				Help: "Total claimed YouTube outbox delivery rows.",
			},
		),
		deliveryProcessedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_delivery_processed_total",
				Help: "Total processed YouTube outbox delivery rows by result.",
			},
			[]string{metricLabelResult},
		),
		dispatchDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_youtube_outbox_dispatch_duration_seconds",
				Help:    "YouTube outbox per-room dispatch duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
		dispatchBatchSize: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_youtube_outbox_dispatch_batch_size",
				Help:    "Claimed YouTube outbox delivery row count per dispatch batch.",
				Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200},
			},
		),
		dispatchTouchedOutboxes: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_youtube_outbox_dispatch_touched_outboxes",
				Help:    "Unique outbox rows touched per YouTube outbox dispatch batch.",
				Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200},
			},
		),
		revivedTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_revived_total",
				Help: "Total fresh never-sent FAILED YouTube outbox rows revived for redelivery.",
			},
		),
		reviveErrorsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_revive_errors_total",
				Help: "Total stale-failed revival sweep transaction errors.",
			},
		),
		deliveryRetryAfterClampedTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_delivery_retry_after_clamped_total",
				Help: "Total YouTube outbox delivery HTTP Retry-After hints clamped to the maximum bound.",
			},
		),
		liveCatchupSuppressionTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_live_catchup_suppression_total",
				Help: "YouTube outbox live catch-up suppression marker lookups by result (suppressed, cache_error, invalid_marker).",
			},
			[]string{metricLabelResult},
		),
		groupedSendFallbackTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_youtube_outbox_grouped_send_fallback_total",
				Help: "YouTube grouped deliveries that fell back to individual sends after a known-not-accepted permanent failure, by result (started, prepare_failed, freeze_failed).",
			},
			[]string{metricLabelResult},
		),
		deliveryTransitionTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_transition_total",
			Help: "Version-fenced YouTube delivery lifecycle operations by operation and durable outcome.",
		}, []string{"operation", metricLabelResult}),
		deliveryRuleTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_rule_total",
			Help: "Applied YouTube delivery lifecycle policy decisions by stable rule ID.",
		}, []string{"rule", metricLabelResult}),
		deliveryLogicalGroupTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_logical_group_total",
			Help: "Ledger-first YouTube logical delivery group resolutions.",
		}, []string{"resolution", metricLabelResult}),
		deliveryOutcomeUnknownTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_outcome_unknown_total",
			Help: "YouTube provider effects whose delivery outcome must remain unknown.",
		}, []string{"transport"}),
		deliveryCommitAdjudication: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_commit_adjudication_total",
			Help: "Primary read-back results after an ambiguous lifecycle commit response.",
		}, []string{"operation", metricLabelResult}),
		deliveryAtomicityBreachTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_atomicity_breach_total",
			Help: "Detected mixed lifecycle command envelopes by bounded operation name.",
		}, []string{"operation"}),
		deliveryLedgerOperationTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_ledger_operation_total",
			Help: "Logical delivery ledger operations by operation and durable result.",
		}, []string{"operation", metricLabelResult}),
		deliveryCleanupGuardTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_delivery_cleanup_guard_total",
			Help: "Terminal outbox cleanup decisions by bounded guard reason and result.",
		}, []string{"reason", metricLabelResult}),
		outboxFanoutTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_fanout_total",
			Help: "Canonical YouTube outbox fanout materialization outcomes.",
		}, []string{metricLabelResult}),
		outboxAggregateProjection: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_outbox_aggregate_projection_total",
			Help: "YouTube outbox aggregate projection outcomes.",
		}, []string{metricLabelResult}),
		outboxAggregateLag: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "hololive_youtube_outbox_aggregate_lag_seconds",
			Help:    "Observed delay from a terminal envelope projection attempt to aggregate convergence.",
			Buckets: prometheus.DefBuckets,
		}),
	}
})

func observeLiveCatchupSuppression(result string) {
	outboxMetrics().liveCatchupSuppressionTotal.WithLabelValues(result).Inc()
}

func observeGroupedSendFallback(result string) {
	outboxMetrics().groupedSendFallbackTotal.WithLabelValues(result).Inc()
}

func observeOutboxRevived(n int64) {
	if n <= 0 {
		return
	}

	outboxMetrics().revivedTotal.Add(float64(n))
}

func observeOutboxReviveError() {
	outboxMetrics().reviveErrorsTotal.Inc()
}

func observeDeliveryRetryAfterClamped() {
	outboxMetrics().deliveryRetryAfterClampedTotal.Inc()
}

func observeOutboxEnqueueOutboxes(result string, n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().enqueueOutboxesTotal.WithLabelValues(result).Add(float64(n))
}

func observeOutboxEnqueueTargetRooms(n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().enqueueTargetRoomsTotal.Add(float64(n))
}

func observeOutboxDispatchDuration(duration time.Duration) {
	outboxMetrics().dispatchDuration.Observe(duration.Seconds())
}

func observeOutboxDeliveryClaimed(n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().deliveryClaimedTotal.Add(float64(n))
}

func observeOutboxDispatchBatchSize(n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().dispatchBatchSize.Observe(float64(n))
}

func observeOutboxDeliveryProcessed(result string, n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().deliveryProcessedTotal.WithLabelValues(result).Add(float64(n))
}

func observeOutboxDispatchTouchedOutboxes(n int) {
	if n <= 0 {
		return
	}

	outboxMetrics().dispatchTouchedOutboxes.Observe(float64(n))
}

func observeLifecycleApply(operation string, result store.ApplyResult, err error, count int) {
	metrics := outboxMetrics()

	if count <= 0 {
		count = 1
	}

	outcome := result.Outcome.String()
	if result.Outcome == 0 {
		outcome = "error"
	}

	metrics.deliveryTransitionTotal.WithLabelValues(operation, outcome).Add(float64(count))

	for _, rule := range result.Rules {
		metrics.deliveryRuleTotal.WithLabelValues(string(rule), outcome).Inc()
	}

	if result.CommitAdjudication != "" {
		metrics.deliveryCommitAdjudication.WithLabelValues(operation, string(result.CommitAdjudication)).Inc()
	}

	if _, ok := errors.AsType[*store.AtomicityBreachError](err); ok {
		metrics.deliveryAtomicityBreachTotal.WithLabelValues(operation).Inc()
	}
}

func observeLogicalResolutions(kinds []preparation.ResolutionKind) {
	metrics := outboxMetrics()

	for _, kind := range kinds {
		result := "resolved"

		if kind == preparation.LogicalInvariantBreach {
			result = "blocked"
		}

		metrics.deliveryLogicalGroupTotal.WithLabelValues(kind.String(), result).Inc()
	}
}

func observeDeliveryOutcomeUnknown(transport string) {
	outboxMetrics().deliveryOutcomeUnknownTotal.WithLabelValues(transport).Inc()
}

func observeLedgerOperation(operation string, result store.ApplyResult, count int) {
	if count <= 0 {
		return
	}

	outboxMetrics().deliveryLedgerOperationTotal.WithLabelValues(operation, result.Outcome.String()).Add(float64(count))
}

func observeCleanupResult(result store.CleanupResult) {
	metrics := outboxMetrics()

	if result.DeletedOutboxes > 0 {
		metrics.deliveryCleanupGuardTotal.WithLabelValues("ledger_backed", "deleted").Add(float64(result.DeletedOutboxes))
	}

	for reason, count := range result.Guards {
		metrics.deliveryCleanupGuardTotal.WithLabelValues(string(reason), "guarded").Add(float64(count))
	}
}

func observeFanoutResult(result store.FanoutResult, err error) {
	observeLifecycleApply("materialize_fanout", result.ApplyResult, err, 1)
	outboxMetrics().outboxFanoutTotal.WithLabelValues(result.Outcome.String()).Inc()
}

func observeAggregateProjection(result string, lag time.Duration) {
	metrics := outboxMetrics()
	metrics.outboxAggregateProjection.WithLabelValues(result).Inc()

	if result == "applied" {
		metrics.outboxAggregateLag.Observe(lag.Seconds())
	}
}
