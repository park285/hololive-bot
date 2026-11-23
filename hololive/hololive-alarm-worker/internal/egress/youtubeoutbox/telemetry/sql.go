package telemetry

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	deleteLoggedBeforeBatchSQL           = mustSQL("delete_logged_before_batch.sql")
	enqueuePreparedChunkSQL              = mustSQL("enqueue_prepared_chunk.sql")
	loadTrackingSnapshotsSQL             = mustSQL("load_tracking_snapshots.sql")
	markLoggedBatchSQL                   = mustSQL("mark_logged_batch.sql")
	markRetryBatchSQL                    = mustSQL("mark_retry_batch.sql")
	persistPostLatencyClassificationsSQL = mustSQL("persist_post_latency_classifications.sql")
	refreshLockedRowsSQL                 = mustSQL("refresh_locked_rows.sql")
	repositoryFetchLockPendingSQL        = mustSQL("repository_fetch_lock_pending.sql")
)

var (
	postDeliveryTimelineByOutboxIDsSQL          = sqlassets.WithFragment(mustSQL("post_delivery_timeline.sql"), "filter", mustSQL("timelines_filter_outbox_ids.sql"))
	postDeliveryTimelineByTrackingIdentitiesSQL = sqlassets.WithFragment(mustSQL("post_delivery_timeline.sql"), "filter", mustSQL("timelines_filter_tracking_identities.sql"))
)
