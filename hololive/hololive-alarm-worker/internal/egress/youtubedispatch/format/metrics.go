package format

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var memberNameMissingTotal = sync.OnceValue(func() prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_youtube_outbox_member_name_missing_total",
		Help: "YouTube notifications rendered with misc/vtuber_fallback because the member has no Korean display name.",
	})
})
