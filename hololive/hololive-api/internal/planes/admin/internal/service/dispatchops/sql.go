package dispatchops

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

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
