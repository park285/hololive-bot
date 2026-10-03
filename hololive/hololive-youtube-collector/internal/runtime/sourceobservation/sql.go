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
	sqlContractBatchCurrent = mustSQL("repository_contract_batch_current_0031_31.sql")
	sqlJobComplete          = mustSQL("repository_job_complete_0011_11.sql")
	sqlJobCompleteError     = mustSQL("repository_job_complete_error_0081_81.sql")
	sqlJobDefer             = mustSQL("repository_job_defer_0082_82.sql")
	sqlProjectionCurrent    = mustSQL("repository_projection_current_0002_02.sql")
	sqlPublishFence         = mustSQL("repository_publish_fence_0001_01.sql")
	sqlPublishSet           = mustSQL("repository_publish_set_0032_32.sql")
	sqlTargetEnabled        = mustSQL("repository_target_enabled_0003_03.sql")
)
