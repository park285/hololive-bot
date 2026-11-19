package store

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	loadTerminalCommunityShortsOutboxResultsSQL  = mustSQL("load_terminal_community_shorts_outbox_results.sql")
	loadTerminalCommunityShortsOutboxResults2SQL = mustSQL("load_terminal_community_shorts_outbox_results_2.sql")
	deliveryLedgerRecordQuarantinedSQL           = mustSQL("delivery_ledger_record_quarantined.sql")
	deliveryLedgerRecordSentSQL                  = mustSQL("delivery_ledger_record_sent.sql")
	deliveryExpiredSnapshotSQL                   = mustSQL("delivery_expired_snapshot.sql")
	findPendingOutboxIDsForAggregateSyncSQL      = mustSQL("find_pending_outbox_ids_for_aggregate_sync.sql")
	deliveryRepositoryAggregateLockSQL           = mustSQL("delivery_repository_aggregate_lock.sql")
	deliveryRepositoryAggregateSyncSQL           = mustSQL("delivery_repository_aggregate_sync.sql")
	loadAlarmSentMarksForDeliveryIDsSQL          = mustSQL("load_alarm_sent_marks_for_delivery_ids.sql")
	fanoutClaimSQL                               = mustSQL("fanout_claim.sql")
	fanoutCleanupExpiredSQL                      = mustSQL("fanout_cleanup_expired.sql")
	fanoutCompleteNoTargetsSQL                   = mustSQL("fanout_complete_no_targets.sql")
	fanoutFailSQL                                = mustSQL("fanout_fail.sql")
	fanoutInsertChildrenSQL                      = mustSQL("fanout_insert_children.sql")
	fanoutLoadChildrenSQL                        = mustSQL("fanout_load_children.sql")
	fanoutLoadOutboxSQL                          = mustSQL("fanout_load_outbox.sql")
	fanoutReleaseOutboxSQL                       = mustSQL("fanout_release_outbox.sql")
	fanoutReviveFailedSQL                        = mustSQL("fanout_revive_failed.sql")
	handoffAdmitSQL                              = mustSQL("handoff_admit.sql")
	handoffAdmitLockSQL                          = mustSQL("handoff_admit_lock.sql")
	handoffClaimDueSQL                           = mustSQL("handoff_claim_due.sql")
	handoffLedgerDeleteSentSQL                   = mustSQL("handoff_ledger_delete_sent.sql")
	handoffMembersSQL                            = mustSQL("handoff_members.sql")
	handoffRequestCloseSQL                       = mustSQL("handoff_request_close.sql")
	handoffRequestReissueSQL                     = mustSQL("handoff_request_reissue.sql")
	handoffRequestStateSQL                       = mustSQL("handoff_request_state.sql")
	sendRequestAdvanceSQL                        = mustSQL("send_request_advance.sql")
	sendRequestBindSQL                           = mustSQL("send_request_bind.sql")
	sendRequestCleanupSQL                        = mustSQL("send_request_cleanup.sql")
	sendRequestFallbackBindSQL                   = mustSQL("send_request_fallback_bind.sql")
	sendRequestFallbackFenceSQL                  = mustSQL("send_request_fallback_fence.sql")
	sendRequestFenceSQL                          = mustSQL("send_request_fence.sql")
	sendRequestGetSQL                            = mustSQL("send_request_get.sql")
	sendRequestInsertSQL                         = mustSQL("send_request_insert.sql")
	sendRequestLoadSQL                           = mustSQL("send_request_load.sql")
	sendRequestLockSQL                           = mustSQL("send_request_lock.sql")
	transitionApplyRowsSQL                       = mustSQL("transition_apply_rows.sql")
	transitionCleanupCandidatesSQL               = mustSQL("transition_cleanup_candidates.sql")
	transitionCleanupChildrenSQL                 = mustSQL("transition_cleanup_children.sql")
	transitionCleanupDeleteSQL                   = mustSQL("transition_cleanup_delete.sql")
	transitionCleanupGroupRowsSQL                = mustSQL("transition_cleanup_group_rows.sql")
	transitionExpiredPendingSQL                  = mustSQL("transition_expired_pending.sql")
	transitionLedgerRowsSQL                      = mustSQL("transition_ledger_rows.sql")
	transitionReadRowsSQL                        = mustSQL("transition_read_rows.sql")
	transitionReviveCandidatesSQL                = mustSQL("transition_revive_candidates.sql")
	transitionStaleSendingSQL                    = mustSQL("transition_stale_sending.sql")
	transitionTelemetryLastAttemptSQL            = mustSQL("transition_telemetry_last_attempt.sql")
)

var (
	transitionClaimPendingSQL = sqlassets.WithFragment(mustSQL("transition_claim_pending.sql"), "ready_predicate", mustSQL("delivery_ready_predicate.sql"))
	deliveryReadySnapshotSQL  = sqlassets.WithFragment(mustSQL("delivery_ready_snapshot.sql"), "ready_predicate", mustSQL("delivery_ready_predicate.sql"))
)

var (
	handoffRequestFenceSQL = mustSQL("handoff_request_fence.sql")
	handoffCompleteSQL     = withHandoffRequestFence("handoff_complete.sql")
	handoffRescheduleSQL   = withHandoffRequestFence("handoff_reschedule.sql")
	handoffRequestLockSQL  = withHandoffRequestFence("handoff_request_lock.sql")
)

func withHandoffRequestFence(name string) string {
	return sqlassets.WithFragment(mustSQL(name), "handoff_request_fence", handoffRequestFenceSQL)
}

var (
	transitionGroupCandidatesSQL = mustSQL("transition_group_candidates.sql")
	transitionGroupRowsSQL       = withGroupCandidates("transition_group_rows.sql")
	handoffGroupParentsSQL       = withGroupCandidates("handoff_group_parents.sql")
)

func withGroupCandidates(name string) string {
	return sqlassets.WithFragment(mustSQL(name), "transition_group_candidates", transitionGroupCandidatesSQL)
}
