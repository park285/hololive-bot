package sourceobservation

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = func() func(string) string {
	read := sqlassets.MustReader(sqlAssets, "queries")
	relation := read("repository_ready_relation.sql")
	claim := strings.ReplaceAll(read("claim_observations.sql"), "{{ready_relation}}", strings.ReplaceAll(relation, "{{max_attempts}}", "$6"))
	snapshot := strings.ReplaceAll(read("repository_ready_snapshot.sql"), "{{ready_relation}}", strings.ReplaceAll(relation, "{{max_attempts}}", "$2"))

	return func(name string) string {
		switch name {
		case "claim_observations.sql":
			return claim
		case "repository_ready_snapshot.sql":
			return snapshot
		default:
			return read(name)
		}
	}
}()

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	activateReplayEpochTx2SQL                       = mustSQL("activate_replay_epoch_tx_2.sql")
	activateReplayEpochTxSQL                        = mustSQL("activate_replay_epoch_tx.sql")
	claimObservationsSQL                            = mustSQL("claim_observations.sql")
	completeFinalizeObservation2SQL                 = mustSQL("complete_finalize_observation_2.sql")
	completeFinalizeObservationSQL                  = mustSQL("complete_finalize_observation.sql")
	contentClockStatementSQL                        = mustSQL("content_clock_statement.sql")
	deleteQueueBatchSQL                             = mustSQL("delete_queue_batch.sql")
	ensureClaimBudgetSQL                            = mustSQL("ensure_claim_budget.sql")
	finalizeNextDueLiveEndTxSQL                     = mustSQL("finalize_next_due_live_end_tx.sql")
	liveHeadStatementSQL                            = mustSQL("live_head_statement.sql")
	loadCommunitySubjectHeadSQL                     = mustSQL("load_community_subject_head.sql")
	loadCommunityWindowReadySQL                     = mustSQL("load_community_window_ready.sql")
	loadContentAbsenceSlotsSQL                      = mustSQL("load_content_absence_slots.sql")
	loadContentClocksSQL                            = mustSQL("load_content_clocks.sql")
	loadContentHeadSQL                              = mustSQL("load_content_head.sql")
	loadContentVideosSQL                            = mustSQL("load_content_videos.sql")
	loadKnownCommunityPostIDsSQL                    = mustSQL("load_known_community_post_ids.sql")
	loadKnownElsewhereSQL                           = mustSQL("load_known_elsewhere.sql")
	loadLiveHeadsSQL                                = mustSQL("load_live_heads.sql")
	loadLiveSessionsSQL                             = mustSQL("load_live_sessions.sql")
	loadPhotoStateSQL                               = mustSQL("load_photo_state.sql")
	loadProfileStateSQL                             = mustSQL("load_profile_state.sql")
	loadTypedWatermarkSQL                           = mustSQL("load_typed_watermark.sql")
	lockClaimSQL                                    = mustSQL("lock_claim.sql")
	lockCommunitySubjectSQL                         = mustSQL("lock_community_subject.sql")
	lockContentSubjectSQL                           = mustSQL("lock_content_subject.sql")
	lockLiveSubjectSQL                              = mustSQL("lock_live_subject.sql")
	persistContentAbsenceSQL                        = mustSQL("persist_content_absence.sql")
	persistContentFieldUpdatesSQL                   = mustSQL("persist_content_field_updates.sql")
	persistContentHeadSQL                           = mustSQL("persist_content_head.sql")
	persistDeadLetterSQL                            = mustSQL("persist_dead_letter.sql")
	persistPhotoHeadSQL                             = mustSQL("persist_photo_head.sql")
	persistPhotoProductSQL                          = mustSQL("persist_photo_product.sql")
	persistPhotoVariantSQL                          = mustSQL("persist_photo_variant.sql")
	persistProfileEvidenceSQL                       = mustSQL("persist_profile_evidence.sql")
	persistProfileHeadSQL                           = mustSQL("persist_profile_head.sql")
	persistReconcileConflictSQL                     = mustSQL("persist_reconcile_conflict.sql")
	repositoryChannelLiveCheckUpsertSQL             = mustSQL("repository_channel_live_check_upsert.sql")
	repositoryChannelLiveNegativeSQL                = mustSQL("repository_channel_live_negative.sql")
	repositoryClaimDeadLetterSQL                    = mustSQL("repository_claim_dead_letter.sql")
	repositoryLiveAbsenceCurrentSlotSQL             = mustSQL("repository_live_absence_current_slot.sql")
	repositoryLiveAbsenceSlotInsertSQL              = mustSQL("repository_live_absence_slot_insert.sql")
	repositoryLiveAbsenceSlotsSQL                   = mustSQL("repository_live_absence_slots.sql")
	repositoryLiveIgnoredAbsencesSQL                = mustSQL("repository_live_ignored_absences.sql")
	repositoryLivePendingEndUpsertSQL               = mustSQL("repository_live_pending_end_upsert.sql")
	repositoryLivePendingEndsDeleteSQL              = mustSQL("repository_live_pending_ends_delete.sql")
	repositoryLivePendingEndsSQL                    = mustSQL("repository_live_pending_ends.sql")
	repositoryLivePremiereClassificationUpsertSQL   = mustSQL("repository_live_premiere_classification_upsert.sql")
	repositoryLiveRuntimeLockSQL                    = mustSQL("repository_live_runtime_lock.sql")
	repositoryLiveRuntimeRefreshSQL                 = mustSQL("repository_live_runtime_refresh.sql")
	repositoryLiveSessionInsertSQL                  = mustSQL("repository_live_session_insert.sql")
	repositoryLiveSessionUpdateSQL                  = mustSQL("repository_live_session_update.sql")
	repositoryLiveTimeFinalizationStateSQL          = mustSQL("repository_live_time_finalization_state.sql")
	repositoryLiveTimeFinalizationWriteSQL          = mustSQL("repository_live_time_finalization_write.sql")
	repositoryReadySnapshotSQL                      = mustSQL("repository_ready_snapshot.sql")
	repositoryRetentionBacklogEvidenceSQL           = mustSQL("repository_retention_backlog_evidence.sql")
	repositoryRetentionDeleteContentAbsenceSlotsSQL = mustSQL("repository_retention_delete_content_absence_slots.sql")
	repositoryRetentionDeleteEvidenceWindowSQL      = mustSQL("repository_retention_delete_evidence_window.sql")
	repositoryRetentionDeleteLiveAbsenceSlotsSQL    = mustSQL("repository_retention_delete_live_absence_slots.sql")
	repositoryRetentionDeletePayloadsSQL            = mustSQL("repository_retention_delete_payloads.sql")
	repositoryRetentionDeletePhotoVariantsSQL       = mustSQL("repository_retention_delete_photo_variants.sql")
	repositoryRetentionDeleteProfileEvidenceSQL     = mustSQL("repository_retention_delete_profile_evidence.sql")
	repositoryRetentionDeleteProvenancesSQL         = mustSQL("repository_retention_delete_provenances.sql")
	repositoryRetryExhaustedSQL                     = mustSQL("repository_retry_exhausted.sql")
	repositoryScheduleHeadsSQL                      = mustSQL("repository_schedule_heads.sql")
	repositoryScheduleSessionsSQL                   = mustSQL("repository_schedule_sessions.sql")
	repositoryVideoAvailabilityUpsertSQL            = mustSQL("repository_video_availability_upsert.sql")
	retentionSteps2SQL                              = mustSQL("retention_steps_2.sql")
	retentionStepsSQL                               = mustSQL("retention_steps.sql")
	retrySQL                                        = mustSQL("retry.sql")
	saveCommunitySubjectHeadSQL                     = mustSQL("save_community_subject_head.sql")
	scheduleItemStatementSQL                        = mustSQL("schedule_item_statement.sql")
	writeFinalizeApplicationsSQL                    = mustSQL("write_finalize_applications.sql")
)
