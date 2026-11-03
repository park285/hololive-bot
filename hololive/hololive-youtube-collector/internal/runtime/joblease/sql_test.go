package joblease

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed testqueries/*
var testSQLAssets embed.FS

var mustTestSQL = sqlassets.MustReader(testSQLAssets, "testqueries")

// 테스트 전용 SQL 자산도 패키지 초기화 때 한 번 읽는다.
var (
	activeLeaseCountSQL       = mustTestSQL("active_lease_count.sql")
	candidate260Bd498868aSQL  = mustTestSQL("candidate_260_bd498868a.sql")
	candidateOrderOracleSQL   = mustTestSQL("candidate_order_oracle.sql")
	carryTargetsSQL           = mustTestSQL("carry_targets.sql")
	clearLeaseMembershipSQL   = mustTestSQL("clear_lease_membership.sql")
	deleteLeaseSQL            = mustTestSQL("delete_lease.sql")
	expireLeaseSQL            = mustTestSQL("expire_lease.sql")
	expireProjectionSQL       = mustTestSQL("expire_projection.sql")
	insertActiveLeaseSQL      = mustTestSQL("insert_active_lease.sql")
	insertProjectionSQL       = mustTestSQL("insert_projection.sql")
	insertTargetSQL           = mustTestSQL("insert_target.sql")
	insertTargetSinceSQL      = mustTestSQL("insert_target_since.sql")
	leaseDeferredStateSQL     = mustTestSQL("lease_deferred_state.sql")
	lockProjectionGuardSQL    = mustTestSQL("lock_projection_guard.sql")
	makeLeaseOverdueSQL       = mustTestSQL("make_lease_overdue.sql")
	makeRetryDueSQL           = mustTestSQL("make_retry_due.sql")
	nullTargetMembershipSQL   = mustTestSQL("null_target_membership.sql")
	projection260Bd498868aSQL = mustTestSQL("projection_260_bd498868a.sql")
	retireProjectionSQL       = mustTestSQL("retire_projection.sql")
	scheduledForIsRecentSQL   = mustTestSQL("scheduled_for_is_recent.sql")
	setNotBeforeSQL           = mustTestSQL("set_not_before.sql")
	shiftIdleSlotSQL          = mustTestSQL("shift_idle_slot.sql")
)
