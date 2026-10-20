package notificationdelivery

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	claimReadySQL    = strings.Replace(mustSQL("outbox_claim_ready.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
	readySnapshotSQL = strings.Replace(mustSQL("outbox_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
)

// handoff 적용 문장은 같은 fence 앞부분(handoff_fence.sql)에 전이별 갱신을 이어 붙인다.
var (
	handoffClaimDueSQL   = mustSQL("handoff_claim_due.sql")
	handoffCompleteSQL   = withHandoffFence("handoff_complete.sql")
	handoffRescheduleSQL = withHandoffFence("handoff_reschedule.sql")
	handoffReissueSQL    = withHandoffFence("handoff_reissue.sql")
	handoffExhaustSQL    = withHandoffFence("handoff_exhaust.sql")
	handoffQuarantineSQL = withHandoffFence("handoff_quarantine.sql")
)

func withHandoffFence(name string) string {
	return strings.Replace(mustSQL(name), "/* handoff_fence */", mustSQL("handoff_fence.sql"), 1)
}
