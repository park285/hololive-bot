package observation

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	applyAlarmSentMarksSQL          = mustSQL("apply_alarm_sent_marks.sql")
	buildSourcePostsBatchUpsert2SQL = mustSQL("build_source_posts_batch_upsert_2.sql")
	buildSourcePostsBatchUpsertSQL  = mustSQL("build_source_posts_batch_upsert.sql")
	findAlarmStateByPostIDSQL       = mustSQL("find_alarm_state_by_post_id.sql")
	findByIdentitySQL               = mustSQL("find_by_identity.sql")
	upsertAlarmStateBatchSQL        = mustSQL("upsert_alarm_state_batch.sql")
	upsertTrackingBatchSQL          = mustSQL("upsert_tracking_batch.sql")
)
