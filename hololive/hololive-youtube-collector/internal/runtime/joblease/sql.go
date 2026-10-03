package joblease

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	sqlCandidates               = mustSQL("repository_candidates_0144_02.sql")
	sqlCandidatesGlobal         = mustSQL("repository_candidates_global_0144_17.sql")
	sqlLeaseAcquire             = mustSQL("repository_lease_acquire_0144_08.sql")
	sqlLeaseComplete            = mustSQL("repository_lease_complete_0144_11.sql")
	sqlLeaseDefer               = mustSQL("repository_lease_defer_0144_12.sql")
	sqlLeaseFailureLock         = mustSQL("repository_lease_failure_lock_0144_14.sql")
	sqlLeaseInsert              = mustSQL("repository_lease_insert_0144_06.sql")
	sqlLeaseRelease             = mustSQL("repository_lease_release_0144_10.sql")
	sqlLeaseRenew               = mustSQL("repository_lease_renew_0144_09.sql")
	sqlProjectionCurrent        = mustSQL("repository_projection_current_0144_01.sql")
	sqlProjectionLock           = mustSQL("repository_projection_lock_0144_05.sql")
	sqlTargetBundle             = mustSQL("repository_target_bundle_0144_04.sql")
	sqlTargetSnapshotExact      = mustSQL("repository_target_snapshot_exact_0144_15.sql")
	sqlTargetSnapshotProjection = mustSQL("repository_target_snapshot_projection_0144_16.sql")
)
