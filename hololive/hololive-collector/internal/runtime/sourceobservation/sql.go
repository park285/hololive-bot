package sourceobservation

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	latestCheckpointCursorSQL    = mustSQL("latest_checkpoint_cursor.sql")
	publishObservationSetSQL     = mustSQL("publish_observation_set.sql")
	publishVerificationBatch2SQL = mustSQL("publish_verification_batch_2.sql")
	publishVerificationBatch3SQL = mustSQL("publish_verification_batch_3.sql")
	publishVerificationBatch4SQL = mustSQL("publish_verification_batch_4.sql")
	publishVerificationBatchSQL  = mustSQL("publish_verification_batch.sql")
)
