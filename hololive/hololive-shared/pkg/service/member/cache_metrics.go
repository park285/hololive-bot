package member

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	memberCacheEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hololive_member_cache_epoch",
		Help: "Most recently reconciled durable member cache epoch in this process.",
	})
	memberCacheEpochReconcileTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_epoch_reconcile_total",
		Help: "Member cache durable epoch reconciliation attempts.",
	}, []string{"reason", "result"})
	memberCacheEpochNotificationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_epoch_notifications_total",
		Help: "Member cache epoch Pub/Sub notification outcomes.",
	}, []string{"result"})
	memberCacheBypassTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_bypass_total",
		Help: "Member cache reads routed directly to PostgreSQL because epoch authority was unavailable.",
	}, []string{"operation", "reason"})
)
