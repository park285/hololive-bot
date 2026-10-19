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
