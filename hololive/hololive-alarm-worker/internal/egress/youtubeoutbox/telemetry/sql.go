package telemetry

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	deleteLoggedBeforeBatchSQL           = mustSQL("delete_logged_before_batch.sql")
	enqueuePreparedChunkSQL              = mustSQL("enqueue_prepared_chunk.sql")
	loadTrackingSnapshotsSQL             = mustSQL("load_tracking_snapshots.sql")
	markLoggedBatchSQL                   = mustSQL("mark_logged_batch.sql")
	markRetryBatchSQL                    = mustSQL("mark_retry_batch.sql")
	persistPostLatencyClassificationsSQL = mustSQL("persist_post_latency_classifications.sql")
	postDeliveryTimelineQuerySQL         = mustSQL("post_delivery_timeline_query.sql")
	refreshLockedRowsSQL                 = mustSQL("refresh_locked_rows.sql")
	repositoryFetchLockPendingSQL        = mustSQL("repository_fetch_lock_pending.sql")
)

// timeline 조회 필터는 두 고정 모양뿐이다(queries/ 아래 SQL asset). 필터별 SQL을 초기화에서 완성해 두므로 목록 길이와
// 무관하게 SQL 문자열이 같아 plan cache를 재사용한다.
var (
	// $2: 대상 outbox id 배열.
	postDeliveryTimelineByOutboxIDsSQL = postDeliveryTimelineQuery(mustSQL("timelines_filter_outbox_ids.sql"))
	// $2/$3: 같은 길이의 (kind, content_id) 쌍 배열.
	postDeliveryTimelineByTrackingIdentitiesSQL = postDeliveryTimelineQuery(mustSQL("timelines_filter_tracking_identities.sql"))
)
