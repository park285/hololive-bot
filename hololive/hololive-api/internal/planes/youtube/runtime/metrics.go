package runtime

import (
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
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

var youtubeQueueSnapshotSuccess = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_queue_snapshot_success",
	Help: "Whether the latest YouTube plane queue observation succeeded; pending/processing/oldest age keep the last successful values on failure.",
})

var youtubeQueueSnapshotLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_queue_snapshot_last_success_timestamp_seconds",
	Help: "Unix time of the last successful YouTube plane queue observation.",
})

var youtubeWorkQueueUtilization = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_work_queue_utilization_ratio",
	Help: "Fraction of the bounded in-process YouTube plane work queue currently occupied.",
})

const (
	consumeResultSuccess         = "success"
	consumeResultRetry           = "retry"
	consumeResultRetryError      = "retry_error"
	consumeResultClaimLost       = "claim_lost"
	consumeResultFatal           = "fatal"
	consumeResultDeadLetter      = "dead_letter"
	consumeResultDeadLetterError = "dead_letter_error"
)

var youtubeConsumeResults = []string{
	consumeResultSuccess,
	consumeResultRetry,
	consumeResultRetryError,
	consumeResultClaimLost,
	consumeResultFatal,
	consumeResultDeadLetter,
	consumeResultDeadLetterError,
}

var youtubeConsumeTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_consume_total",
	Help: "YouTube plane consume outcomes after collector publish.",
}, []string{"result"}), youtubeConsumeResults)

const (
	projectionRetentionLeasesTable      = "youtube_collection_job_leases"
	projectionRetentionReasonsTable     = "youtube_collection_target_reasons"
	projectionRetentionTargetsTable     = "youtube_collection_targets"
	projectionRetentionGenerationsTable = "youtube_collection_projection_generations"
	retentionUnknownTable               = "none"
)

var youtubeRetentionTables = slices.Concat(sourceobservation.RetentionTables(), []string{
	projectionRetentionLeasesTable,
	projectionRetentionReasonsTable,
	projectionRetentionTargetsTable,
	projectionRetentionGenerationsTable,
})

var youtubeRetentionDeletedTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_deleted_total",
	Help: "Total rows deleted by the YouTube plane retention worker.",
}, []string{"table"}), youtubeRetentionTables)

var youtubeRetentionSourceScannedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_source_scanned_total",
	Help: "Expired source observations the retention cursor moved past, including deleted ones; subtracting the source_observations deleted total gives the rows kept as protected or locked.",
})

var youtubeRetentionSourceTraversalsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_source_traversals_total",
	Help: "Completed source observation retention traversals, counted when the cursor returns to the start.",
})

var youtubeRetentionErrorsTotal = precreateCounterVec(promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_retention_errors_total",
	Help: "Total YouTube plane retention ticks that failed.",
}, []string{"table"}), slices.Concat(youtubeRetentionTables, []string{retentionUnknownTable}))

const (
	retentionPhaseSource     = "source"
	retentionPhaseProjection = "projection"
)

var youtubeRetentionTickSeconds = precreateObserverVec(promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "hololive_youtube_plane_retention_tick_seconds",
	Help: "Wall time of one YouTube plane retention phase after its DB slot is acquired; slot wait is in hololive_youtube_plane_db_slot_wait_seconds.",
}, []string{"phase"}), []string{retentionPhaseSource, retentionPhaseProjection})

type dbOperation string

const (
	dbOperationProbe               dbOperation = "probe"
	dbOperationClaim               dbOperation = "claim"
	dbOperationConsume             dbOperation = "consume"
	dbOperationRetry               dbOperation = "retry"
	dbOperationDeadLetter          dbOperation = "dead_letter"
	dbOperationLiveEnd             dbOperation = "live_end"
	dbOperationProjectionRefresh   dbOperation = "projection_refresh"
	dbOperationRetentionSource     dbOperation = "retention_source"
	dbOperationRetentionProjection dbOperation = "retention_projection"
	dbOperationQueueSnapshot       dbOperation = "queue_snapshot"
	dbOperationCollectionSnapshot  dbOperation = "collection_snapshot"
)

var dbOperationLabels = []string{
	string(dbOperationProbe),
	string(dbOperationClaim),
	string(dbOperationConsume),
	string(dbOperationRetry),
	string(dbOperationDeadLetter),
	string(dbOperationLiveEnd),
	string(dbOperationProjectionRefresh),
	string(dbOperationRetentionSource),
	string(dbOperationRetentionProjection),
	string(dbOperationQueueSnapshot),
	string(dbOperationCollectionSnapshot),
}

var dbSlotBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

var youtubeDBSlotWaitSeconds = precreateObserverVec(promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "hololive_youtube_plane_db_slot_wait_seconds",
	Help:    "Time from requesting a YouTube plane DB slot until it was acquired or the context ended; failed acquisitions are included.",
	Buckets: dbSlotBuckets,
}, []string{"operation"}), dbOperationLabels)

var youtubeDBSlotHoldSeconds = precreateObserverVec(promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "hololive_youtube_plane_db_slot_hold_seconds",
	Help:    "Time a YouTube plane DB slot was held from acquisition until release.",
	Buckets: dbSlotBuckets,
}, []string{"operation"}), dbOperationLabels)

func observeSeconds(vec *prometheus.HistogramVec, label string, started time.Time) {
	vec.WithLabelValues(label).Observe(time.Since(started).Seconds())
}

func precreateCounterVec(vec *prometheus.CounterVec, values []string) *prometheus.CounterVec {
	for _, value := range values {
		vec.WithLabelValues(value)
	}

	return vec
}

func precreateObserverVec(vec *prometheus.HistogramVec, values []string) *prometheus.HistogramVec {
	for _, value := range values {
		vec.WithLabelValues(value)
	}

	return vec
}

var youtubeRetentionBacklogAgeSeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "hololive_youtube_plane_retention_backlog_age_seconds",
	Help: "Age of the oldest eligible source observation after retention; zero when empty, absent when unknown.",
}, []string{"table"})
