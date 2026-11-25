package dispatchoutbox

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	ensureSendUnitsSQL           = mustSQL("ensure_send_units.sql")
	insertDeliveryBatchSQL       = mustSQL("insert_delivery_batch.sql")
	insertEventBatchSQL          = mustSQL("insert_event_batch.sql")
	loadEventsByIDSQL            = mustSQL("load_events_by_id.sql")
	loadExistingEventRowsSQL     = mustSQL("load_existing_event_rows.sql")
	quarantineStaleSendingSQL    = mustSQL("quarantine_stale_sending.sql")
	recordEventCollisionsSQL     = mustSQL("record_event_collisions.sql")
	recoverExpiredLeasedSQL      = mustSQL("recover_expired_leased.sql")
	repositoryPublishReceiptsSQL = mustSQL("repository_publish_receipts.sql")
	sendRequestLockSQL           = mustSQL("send_request_lock.sql")
	sendRequestPinSQL            = mustSQL("send_request_pin.sql")
	upcomingCandidatesApplySQL   = mustSQL("upcoming_candidates_apply.sql")
	upcomingCandidatesCleanupSQL = mustSQL("upcoming_candidates_cleanup.sql")
	upcomingCandidatesPendingSQL = mustSQL("upcoming_candidates_pending.sql")
	upcomingCandidatesStageSQL   = mustSQL("upcoming_candidates_stage.sql")
)

var (
	routeFailuresSQL                = mustSQL("route_failures.sql")
	routeSendingFailuresSQL         = mustSQL("route_sending_failures.sql")
	requeuePreSendSQL               = mustSQL("requeue_pre_send.sql")
	markSentSQL                     = mustSQL("mark_sent.sql")
	releaseLeasedSQL                = mustSQL("release_leased.sql")
	repositoryTerminalDLQSQL        = mustSQL("repository_terminal_dlq.sql")
	repositoryTerminalQuarantineSQL = mustSQL("repository_terminal_quarantine.sql")
	sendRequestMembersSQL           = mustSQL("send_request_members.sql")
	sendRequestBeginSQL             = mustSQL("send_request_begin.sql")
	sendRequestReissueSQL           = mustSQL("send_request_reissue.sql")
	claimDueSQL                     = mustSQL("claim_due.sql").With(sqlassets.Fragment("ready_predicate"), mustSQL("delivery_ready_predicate.sql"))
	deliveryReadySnapshotSQL        = mustSQL("delivery_ready_snapshot.sql").With(sqlassets.Fragment("ready_predicate"), mustSQL("delivery_ready_predicate.sql"))
)

var (
	handoffFenceSQL      = mustSQL("handoff_fence.sql")
	handoffClaimDueSQL   = mustSQL("handoff_claim_due.sql")
	handoffCompleteSQL   = withHandoffFence("handoff_complete.sql")
	handoffRescheduleSQL = withHandoffFence("handoff_reschedule.sql")
	handoffReissueSQL    = withHandoffFence("handoff_reissue.sql")
	handoffDLQSQL        = withHandoffFence("handoff_dlq.sql")
	handoffQuarantineSQL = withHandoffFence("handoff_quarantine.sql")
	handoffRoomPauseSQL  = withHandoffFence("handoff_room_not_joined.sql")
)

func withHandoffFence(name string) pgxdb.Statement {
	return mustSQL(name).With(sqlassets.Fragment("handoff_fence"), handoffFenceSQL)
}
