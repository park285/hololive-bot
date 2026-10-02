package messagestrings

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	lookupMissReasonUnloaded = "unloaded"
	lookupMissReasonMissing  = "missing"
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
	NamespaceTimeFmt,
}

var (
	metricsInitOnce sync.Once

	loadFailuresTotal prometheus.Counter
	lookupMissTotal   *prometheus.CounterVec
)

func initMetrics() {
	metricsInitOnce.Do(func() {
		loadFailuresTotal = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_messagestrings_load_failures_total",
				Help: "Total failed message_strings loads from PostgreSQL at runtime startup.",
			},
		)
		lookupMissTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_messagestrings_lookup_miss_total",
				Help: "Total message_strings lookups that found no value, by reason (unloaded: Load was not called, missing: namespace/key absent) and namespace. Required keys are validated at startup, so increases on validated keys indicate a wiring defect; dynamic label lookups use the raw value.",
			},
			[]string{"reason", "namespace"},
		)

		for _, reason := range []string{lookupMissReasonUnloaded, lookupMissReasonMissing} {
			for _, namespace := range knownNamespaces {
				lookupMissTotal.WithLabelValues(reason, namespace)
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

func observeLookupMiss(reason, namespace string) {
	initMetrics()

	if lookupMissTotal == nil {
		return
	}

	lookupMissTotal.WithLabelValues(reason, namespace).Inc()
}
