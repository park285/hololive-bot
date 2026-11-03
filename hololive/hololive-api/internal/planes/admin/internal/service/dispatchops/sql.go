package dispatchops

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽고 오류 분류식 자리도 그때 채웁니다. 요청마다 자산을 다시 읽거나 치환하지
// 않으며, 없는 자산은 admin 요청이 아니라 기동에서 드러납니다.
var (
	actionsSQL     = mustSQL("actions.sql")
	detailSQL      = mustSQL("detail.sql")
	failuresSQL    = withErrorCodeExpression(mustSQL("failures.sql"))
	groupLockedSQL = mustSQL("group_locked.sql")
	groupSQL       = mustSQL("group.sql")
	listSQL        = withErrorCodeExpression(mustSQL("list.sql"))
	requeueSQL     = mustSQL("requeue.sql")
	settleSQL      = mustSQL("settle.sql")
	summarySQL     = mustSQL("summary.sql")
)

func withErrorCodeExpression(query string) string {
	return strings.ReplaceAll(query, "{{errorCodeExpression}}", errorCodeExpression)
}
