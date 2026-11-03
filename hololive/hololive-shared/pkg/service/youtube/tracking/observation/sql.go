package observation

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산과 동적 upsert 문장의 고정 조각은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	applyAlarmSentMarksSQL          = mustSQL("apply_alarm_sent_marks.sql")
	buildAlarmStateUpsertQuery2SQL  = mustSQL("build_alarm_state_upsert_query_2.sql")
	buildAlarmStateUpsertQuerySQL   = mustSQL("build_alarm_state_upsert_query.sql")
	buildSourcePostsBatchUpsert2SQL = mustSQL("build_source_posts_batch_upsert_2.sql")
	buildSourcePostsBatchUpsertSQL  = mustSQL("build_source_posts_batch_upsert.sql")
	buildTrackingUpsertQuery2SQL    = mustSQL("build_tracking_upsert_query_2.sql")
	buildTrackingUpsertQuerySQL     = mustSQL("build_tracking_upsert_query.sql")
	findAlarmStateByPostIDSQL       = mustSQL("find_alarm_state_by_post_id.sql")
	findByIdentitySQL               = mustSQL("find_by_identity.sql")
)
