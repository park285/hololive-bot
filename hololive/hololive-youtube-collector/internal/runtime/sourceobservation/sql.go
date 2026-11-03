package sourceobservation

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	completeCollectionJobSQL          = mustSQL("complete_collection_job.sql")
	completeCollectionJobWithErrorSQL = mustSQL("complete_collection_job_with_error.sql")
	deferCollectionJobSQL             = mustSQL("defer_collection_job.sql")
	latestCheckpointCursorSQL         = mustSQL("latest_checkpoint_cursor.sql")
	publishObservationSetSQL          = mustSQL("publish_observation_set.sql")
	publishVerificationBatch2SQL      = mustSQL("publish_verification_batch_2.sql")
	publishVerificationBatch3SQL      = mustSQL("publish_verification_batch_3.sql")
	publishVerificationBatch4SQL      = mustSQL("publish_verification_batch_4.sql")
	publishVerificationBatchSQL       = mustSQL("publish_verification_batch.sql")
)
