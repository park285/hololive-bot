package dispatchops

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽고 오류 분류식과 행 projection 자리도 그때 채웁니다. 요청마다 자산을 다시 읽거나
// 치환하지 않으며, 없는 자산은 admin 요청이 아니라 기동에서 드러납니다.
// 목록·상세·묶음 조회는 list.sql의 발송 행 projection을 함께 쓰며, 그 열 순서는 scanDelivery와 같아야 합니다.
var (
	actionsSQL     = mustSQL("actions.sql")
	detailSQL      = withListProjection(mustSQL("detail.sql"))
	failuresSQL    = withErrorCodeExpression(mustSQL("failures.sql"))
	groupLockedSQL = groupSQL + "FOR UPDATE OF d\n"
	groupSQL       = withListProjection(mustSQL("group.sql"))
	listSQL        = withErrorCodeExpression(mustSQL("list.sql"))
	requeueSQL     = mustSQL("requeue.sql")
	settleSQL      = mustSQL("settle.sql")
	summarySQL     = mustSQL("summary.sql")
)

func withErrorCodeExpression(query string) string {
	return strings.ReplaceAll(query, "{{errorCodeExpression}}", errorCodeExpression)
}

func withListProjection(query string) string {
	return strings.ReplaceAll(query, "{{listProjection}}", listSQL)
}
