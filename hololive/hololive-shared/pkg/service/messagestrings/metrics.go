package messagestrings

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	fallbackReasonUnloaded = "unloaded"
	fallbackReasonMissing  = "missing"
)

var knownNamespaces = []string{
	NamespaceOrg,
	NamespaceAlarmType,
	NamespaceNewsCat,
	NamespaceSocial,
	NamespaceMisc,
	NamespaceError,
	NamespaceNotify,
	NamespaceCalendar,
	NamespaceLiveCard,
	NamespaceProfileCard,
	NamespaceRankCard,
	NamespaceTimeFmt,
	NamespaceKaring,
}

var (
	metricsInitOnce sync.Once

	loadFailuresTotal   prometheus.Counter
	lookupFallbackTotal *prometheus.CounterVec
)

func initMetrics() {
	metricsInitOnce.Do(func() {
		loadFailuresTotal = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_messagestrings_load_failures_total",
				Help: "Total failed message_strings loads from PostgreSQL (explicit Load or lazy load on lookup).",
			},
		)
		lookupFallbackTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_messagestrings_lookup_fallback_total",
				Help: "Total message_strings lookups that returned empty so the caller used a code-side fallback, by reason (unloaded: store not loaded, missing: namespace/key absent) and namespace.",
			},
			[]string{"reason", "namespace"},
		)

		for _, reason := range []string{fallbackReasonUnloaded, fallbackReasonMissing} {
			for _, namespace := range knownNamespaces {
				lookupFallbackTotal.WithLabelValues(reason, namespace)
			}
		}
	})
}

func observeLoadFailure() {
	initMetrics()

	if loadFailuresTotal == nil {
		return
	}

	loadFailuresTotal.Inc()
}

func observeLookupFallback(reason, namespace string) {
	initMetrics()

	if lookupFallbackTotal == nil {
		return
	}

	lookupFallbackTotal.WithLabelValues(reason, namespace).Inc()
}
