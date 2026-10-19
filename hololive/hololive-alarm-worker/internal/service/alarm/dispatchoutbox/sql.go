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
	sqlClaimDue             = strings.ReplaceAll(mustSQL("repository_claim_0053_02.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"))
	sqlReadySnapshot        = strings.Replace(mustSQL("delivery_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
)
