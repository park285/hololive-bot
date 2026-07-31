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
	replyOutboxAcceptedReclaimedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bot_reply_outbox_accepted_reclaimed_total",
		Help: "Accepted reply outbox leases moved to manual review after expiry.",
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
)
