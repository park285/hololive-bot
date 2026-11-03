package runtime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	collectionTargetObservabilitySQL = mustSQL("collection_target_observability.sql")
	liveCheckVideosSQL               = mustSQL("live_check_videos.sql")
	notificationChannelIDsSQL        = mustSQL("notification_channel_ids.sql")
	operationalChannelIDsSQL         = mustSQL("operational_channel_ids.sql")
	queueObservabilitySQL            = mustSQL("queue_observability.sql")
	timeFinalizationObservabilitySQL = mustSQL("time_finalization_observability.sql")
	timeFinalizationVideosSQL        = mustSQL("time_finalization_videos.sql")
)
