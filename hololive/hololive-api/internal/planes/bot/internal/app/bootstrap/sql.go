package bootstrap

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var reserveBotWebhookNonceSQL = sqlassets.MustStatementReader(sqlAssets, "queries")("reserve_bot_webhook_nonce.sql")
