package botruntime

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	durableClaimIdleTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bot_durable_claim_idle_total",
		Help: "Durable queue claims that found no immediately processable row or returned an error.",
	}, []string{"queue"})
	durableOwnershipCancellationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bot_durable_ownership_cancellation_total",
		Help: "Durable commands canceled before lease expiry, partitioned by bounded ownership reason.",
	}, []string{"reason"})
	durableCommandOutcomeUnknownTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bot_durable_command_outcome_unknown_total",
		Help: "Durable command executions closed without a confirmed side-effect outcome.",
	})
	// 이전 바이너리가 lease를 남긴 채 기록한 accepted만 센다. Lease 없는 accepted는 handoff reconciler가 맡는다.
	replyOutboxAcceptedReclaimedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bot_reply_outbox_accepted_reclaimed_total",
		Help: "Legacy leased accepted reply outbox rows moved to manual review after lease expiry.",
	})
	replyOutboxManualReviewBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bot_reply_outbox_manual_review_backlog",
		Help: "Reply outbox rows requiring manual review.",
	})
	replyOutboxManualReviewOldestAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bot_reply_outbox_manual_review_oldest_age_seconds",
		Help: "Age in seconds of the oldest reply outbox row requiring manual review.",
	})
	replyOutboxManualReviewObservationFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bot_reply_outbox_manual_review_observation_failures_total",
		Help: "Failures while observing reply outbox manual-review backlog.",
	})
	replyOutboxManualReviewSnapshotSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bot_reply_outbox_manual_review_snapshot_success",
		Help: "Whether the latest reply outbox manual-review observation succeeded; counts keep the last successful values on failure.",
	})
	replyOutboxManualReviewSnapshotLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bot_reply_outbox_manual_review_snapshot_last_success_timestamp_seconds",
		Help: "Unix time of the last successful reply outbox manual-review observation.",
	})
)
