package dispatchoutbox

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 반복 실패 처리에서는 고정 SQL 자산을 다시 읽거나 문자열로 복사하지 않는다.
var (
	sqlRouteFailures        = mustSQL("repository_transitions_0160_06.sql")
	sqlRouteSendingFailures = mustSQL("repository_transitions_0170_07.sql")
	sqlRequeuePreSend       = mustSQL("repository_transitions_0185_08.sql")
	sqlMarkSent             = mustSQL("repository_transitions_0040_02.sql")
	sqlReleaseLeased        = mustSQL("repository_transitions_0152_05.sql")
	sqlMoveToDLQ            = mustSQL("repository_terminal_dlq.sql")
	sqlQuarantine           = mustSQL("repository_terminal_quarantine.sql")
	sqlSendRequestMembers   = mustSQL("send_request_members.sql")
	sqlBeginSendRequest     = mustSQL("send_request_begin.sql")
	sqlReissueSendRequest   = mustSQL("send_request_reissue.sql")
	sqlClaimDue             = strings.Replace(mustSQL("repository_claim_0053_02.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
	sqlReadySnapshot        = strings.Replace(mustSQL("delivery_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
)

// handoff 적용 문장은 같은 fence 앞부분(handoff_fence.sql)에 전이별 갱신을 이어 붙인다.
var (
	sqlHandoffClaimDue   = mustSQL("handoff_claim_due.sql")
	sqlHandoffComplete   = withHandoffFence("handoff_complete.sql")
	sqlHandoffReschedule = withHandoffFence("handoff_reschedule.sql")
	sqlHandoffReissue    = withHandoffFence("handoff_reissue.sql")
	sqlHandoffDLQ        = withHandoffFence("handoff_dlq.sql")
	sqlHandoffQuarantine = withHandoffFence("handoff_quarantine.sql")
)

func withHandoffFence(name string) string {
	return strings.Replace(mustSQL(name), "/* handoff_fence */", mustSQL("handoff_fence.sql"), 1)
}
