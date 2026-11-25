package dispatchops

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	mustSQL       = sqlassets.MustReader(sqlAssets, "queries")
	mustStatement = sqlassets.MustStatementReader(sqlAssets, "queries")
)

var (
	errorCodeExpression = strings.TrimSpace(mustSQL("error_code_expression.sql"))
	listSQL             = withErrorCodeExpression(mustSQL("list.sql"))
)

var (
	errorCodeStatement = mustStatement("error_code_expression.sql")
	listStatement      = mustStatement("list.sql").With("{{errorCodeExpression}}", errorCodeStatement)
	actionsSQL         = mustStatement("actions.sql")
	detailSQL          = mustStatement("detail.sql").With("{{listProjection}}", listStatement)
	failuresSQL        = mustStatement("failures.sql").With("{{errorCodeExpression}}", errorCodeStatement)
	groupLockedSQL     = mustStatement("group_locked.sql").With("{{listProjection}}", listStatement)
	groupSQL           = mustStatement("group.sql").With("{{listProjection}}", listStatement)
	requeueSQL         = mustStatement("requeue.sql")
	settleSQL          = mustStatement("settle.sql")
	summarySQL         = mustStatement("summary.sql")
)

func withErrorCodeExpression(query string) string {
	return strings.ReplaceAll(query, "{{errorCodeExpression}}", errorCodeExpression)
}
