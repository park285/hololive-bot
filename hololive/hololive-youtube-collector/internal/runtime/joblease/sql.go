package joblease

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 초기화 때 한 번 읽는다. 획득 문장은 범위별 bundle과 공통 선점 CTE를 결합한다.
var (
	sqlCandidates               = mustSQL("repository_candidates_0144_02.sql")
	sqlCandidatesGlobal         = mustSQL("repository_candidates_global_0144_17.sql")
	sqlLeaseAcquire             = "WITH bundle AS MATERIALIZED (\n" + mustSQL("repository_target_bundle_0144_04.sql") + "\n),\n" + mustSQL("repository_lease_acquire_0144_08.sql")
	sqlLeaseAcquireExact        = "WITH bundle AS MATERIALIZED (\n" + mustSQL("repository_target_bundle_exact.sql") + "\n),\n" + mustSQL("repository_lease_acquire_0144_08.sql")
	sqlLeaseInitialize          = mustSQL("repository_lease_initialize.sql")
	sqlLeaseComplete            = mustSQL("repository_lease_complete_0144_11.sql")
	sqlLeaseDefer               = mustSQL("repository_lease_defer_0144_12.sql")
	sqlLeaseFailureLock         = mustSQL("repository_lease_failure_lock_0144_14.sql")
	sqlLeaseReleaseSuperseded   = mustSQL("repository_lease_release_superseded.sql")
	sqlLeaseRelease             = mustSQL("repository_lease_release_0144_10.sql")
	sqlLeaseRenew               = mustSQL("repository_lease_renew_0144_09.sql")
	sqlProjectionCurrent        = mustSQL("repository_projection_current_0144_01.sql")
	sqlProjectionLock           = mustSQL("repository_projection_lock_0144_05.sql")
	sqlTargetSnapshotExact      = mustSQL("repository_target_snapshot_exact_0144_15.sql")
	sqlTargetSnapshotProjection = mustSQL("repository_target_snapshot_projection_0144_16.sql")
)
