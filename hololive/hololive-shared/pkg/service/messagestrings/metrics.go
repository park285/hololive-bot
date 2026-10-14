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

	metricsInitOnce sync.Once
)

// initMetrics는 알려진 이유·namespace 조합을 0으로 미리 만들어 첫 miss도 increase()에 잡히게 한다.
func initMetrics() {
	metricsInitOnce.Do(func() {
		for _, reason := range []string{lookupMissReasonUnloaded, lookupMissReasonMissing} {
			for _, namespace := range knownNamespaces {
				lookupMissTotal.WithLabelValues(reason, namespace)
			}
		}
	})
}

func observeLoadFailure() {
	initMetrics()
	loadFailuresTotal.Inc()
}

func observeLookupMiss(reason, namespace string) {
	initMetrics()
	lookupMissTotal.WithLabelValues(reason, namespace).Inc()
}
