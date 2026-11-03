package targetprojection

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	activateStagingGenerationSQL = mustSQL("activate_staging_generation.sql")
	deleteReasonsSQL             = mustSQL("delete_reasons.sql")
	deleteRetiredGenerationsSQL  = mustSQL("delete_retired_generations.sql")
	deleteRetiredJobLeasesSQL    = mustSQL("delete_retired_job_leases.sql")
	extendGenerationValiditySQL  = mustSQL("extend_generation_validity.sql")
	extendTargetsSQL             = mustSQL("extend_targets.sql")
	insertReasonsSQL             = mustSQL("insert_reasons.sql")
	insertStagingGenerationSQL   = mustSQL("insert_staging_generation.sql")
	insertTargetsSQL             = mustSQL("insert_targets.sql")
	loadReasonsSQL               = mustSQL("load_reasons.sql")
	loadTargetsSQL               = mustSQL("load_targets.sql")
	lockCurrentGenerationSQL     = mustSQL("lock_current_generation.sql")
	lockProjectionGuardSQL       = mustSQL("lock_projection_guard.sql")
	retireCurrentGenerationSQL   = mustSQL("retire_current_generation.sql")
)
