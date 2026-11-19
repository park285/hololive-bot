package handlers

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	broadcastHistoryListPageTemplateSQL = mustSQL("broadcast_history_list_page_template.sql")
	getEndedBroadcastSQL                = mustSQL("get_ended_broadcast.sql")
)

var (
	broadcastHistoryAllChannelsPageSQL = broadcastHistoryListPageSQL(`$1::text = ''`)
	broadcastHistoryChannelPageSQL     = broadcastHistoryListPageSQL(`s.channel_id = $1`)
)

func broadcastHistoryListPageSQL(channelPredicate string) string {
	return strings.ReplaceAll(broadcastHistoryListPageTemplateSQL, "{{channelPredicate}}", channelPredicate)
}
