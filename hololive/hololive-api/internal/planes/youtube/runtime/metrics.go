package runtime

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var youtubeClaimLostTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_claim_lost_total",
	Help: "Total YouTube plane observations skipped because their claim was no longer owned.",
})

var youtubeFinalizeTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_finalize_total",
	Help: "Total YouTube plane observations consumed after collector publish.",
})

var youtubePendingQueue = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_pending_queue",
	Help: "Pending and processing source observation queue depth.",
})

var youtubeProcessingQueue = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_processing_queue",
	Help: "Source observations currently owned by YouTube plane workers.",
})

var youtubeQueueOldestAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_queue_oldest_age_seconds",
	Help: "Age of the oldest pending or processing source observation.",
})

var youtubeWorkQueueUtilization = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_work_queue_utilization_ratio",
	Help: "Fraction of the bounded in-process YouTube plane work queue currently occupied.",
})

var youtubeConsumeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_consume_total",
	Help: "YouTube plane consume outcomes after collector publish.",
}, []string{"result"})

var youtubeRetentionDeletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_deleted_total",
	Help: "Total rows deleted by the YouTube plane retention worker.",
}, []string{"table"})

var youtubeRetentionErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_errors_total",
	Help: "Total YouTube plane retention ticks that failed.",
}, []string{"table"})

var youtubeRetentionTickSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
	Name: "hololive_youtube_plane_retention_tick_seconds",
	Help: "Wall time of one YouTube plane retention tick.",
})

var youtubeRetentionBacklogAgeSeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_retention_backlog_age_seconds",
	Help: "Age of remaining eligible retention rows after a full batch.",
}, []string{"table"})
