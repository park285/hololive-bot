package runtime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	collectionTargetObservabilitySQL = mustSQL("collection_target_observability.sql")
	liveCheckVideosSQL               = mustSQL("live_check_videos.sql")
	notificationChannelIDsSQL        = mustSQL("notification_channel_ids.sql")
	operationalChannelIDsSQL         = mustSQL("operational_channel_ids.sql")
	queueObservabilitySQL            = mustSQL("queue_observability.sql")
	timeFinalizationObservabilitySQL = mustSQL("time_finalization_observability.sql")
	timeFinalizationVideosSQL        = mustSQL("time_finalization_videos.sql")
)
