package handlers

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	broadcastHistoryListPageTemplateSQL = mustSQL("broadcast_history_list_page_template.sql")
	getEndedBroadcastSQL                = mustSQL("get_ended_broadcast.sql")
)

var (
	broadcastHistoryAllChannelsPageSQL = broadcastHistoryListPageTemplateSQL.WithToken("{{channelPredicate}}", `$1::text = ''`)
	broadcastHistoryChannelPageSQL     = broadcastHistoryListPageTemplateSQL.WithToken("{{channelPredicate}}", `s.channel_id = $1`)
)
