package handlers

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 채널 조건만 다른 두 방송 이력 목록 문장은 broadcast_history_list_page_template.sql 원본을 함께 쓴다.
var (
	broadcastHistoryListPageTemplateSQL = mustSQL("broadcast_history_list_page_template.sql")
	getEndedBroadcastSQL                = mustSQL("get_ended_broadcast.sql")
)

// 전체·채널 목록을 다른 문장으로 준비합니다. 빈 채널이면 참이 되는 OR 조건으로 한 문장에 두면 generic plan에서
// 채널 조건이 채널 정렬 인덱스의 key가 되지 못하고 다른 채널 행을 건너뛰며 읽습니다. 전체 목록의 $1 조건은
// 매개변수 형식을 정하고 빈 채널로만 호출됨을 확인합니다.
var (
	broadcastHistoryAllChannelsPageSQL = broadcastHistoryListPageSQL(`$1::text = ''`)
	broadcastHistoryChannelPageSQL     = broadcastHistoryListPageSQL(`s.channel_id = $1`)
)

func broadcastHistoryListPageSQL(channelPredicate string) string {
	return strings.ReplaceAll(broadcastHistoryListPageTemplateSQL, "{{channelPredicate}}", channelPredicate)
}
