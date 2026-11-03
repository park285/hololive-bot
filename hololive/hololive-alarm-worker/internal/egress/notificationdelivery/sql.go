package notificationdelivery

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	cleanupInBatchesSQL           = mustSQL("cleanup_in_batches.sql")
	countFailedSQL                = mustSQL("count_failed.sql")
	markFailedSQL                 = mustSQL("mark_failed.sql")
	markSendingSQL                = mustSQL("mark_sending.sql")
	markSentSQL                   = mustSQL("mark_sent.sql")
	outboxMarkQuarantinedSQL      = mustSQL("outbox_mark_quarantined.sql")
	outboxReissueFailedRequestSQL = mustSQL("outbox_reissue_failed_request.sql")
	outboxSaveRequestSQL          = mustSQL("outbox_save_request.sql")
	quarantineStaleSendingSQL     = mustSQL("quarantine_stale_sending.sql")
)

var (
	outboxClaimReadySQL    = strings.Replace(mustSQL("outbox_claim_ready.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
	outboxReadySnapshotSQL = strings.Replace(mustSQL("outbox_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
)

// handoff 적용 문장은 같은 fence 앞부분(handoff_fence.sql)에 전이별 갱신을 이어 붙인다.
var (
	handoffFenceSQL      = mustSQL("handoff_fence.sql")
	handoffClaimDueSQL   = mustSQL("handoff_claim_due.sql")
	handoffCompleteSQL   = withHandoffFence("handoff_complete.sql")
	handoffRescheduleSQL = withHandoffFence("handoff_reschedule.sql")
	handoffReissueSQL    = withHandoffFence("handoff_reissue.sql")
	handoffExhaustSQL    = withHandoffFence("handoff_exhaust.sql")
	handoffQuarantineSQL = withHandoffFence("handoff_quarantine.sql")
)

func withHandoffFence(name string) string {
	return strings.Replace(mustSQL(name), "/* handoff_fence */", handoffFenceSQL, 1)
}
