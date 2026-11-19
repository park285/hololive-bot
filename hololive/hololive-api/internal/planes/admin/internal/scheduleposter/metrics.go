package scheduleposter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const (
	rowsResultSucceeded  = "succeeded"
	rowsResultFailed     = "failed"
	rowsResultStale      = "stale"
	rowsResultStoreError = "store_error"
)

const (
	changeCycleCompleted  = "completed"
	changeCyclePaused     = "paused"
	changeCycleStoreError = "store_error"
)

var rowsTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_schedule_poster_rows_total",
	Help: "Schedule poster row extraction outcomes per poster.",
}, []string{"result"}), rowsResultSucceeded, rowsResultFailed, rowsResultStale, rowsResultStoreError)

var postsTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_schedule_poster_posts_total",
	Help: "Schedule poster change detection outcomes per post.",
}, []string{"outcome"}),
	string(posterstore.PostNotRelevant), string(posterstore.PostRelevant), string(posterstore.PostMatched),
	string(posterstore.PostJudgeFailed), string(posterstore.PostMatchFailed))

var changeCyclesTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_schedule_poster_change_cycles_total",
	Help: "Schedule poster change detection cycles by result.",
}, []string{"result"}), changeCycleCompleted, changeCyclePaused, changeCycleStoreError)

var changeLastCompleted = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_schedule_poster_change_last_completed_timestamp_seconds",
	Help: "Unix time of the last completed schedule poster change detection cycle.",
})

func precreateCounterVec(vec *prometheus.CounterVec, values ...string) *prometheus.CounterVec {
	for _, value := range values {
		vec.WithLabelValues(value)
	}

	return vec
}
