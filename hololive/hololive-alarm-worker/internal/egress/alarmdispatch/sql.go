package alarmdispatch

import (
	"embed"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	acquireAlarmDispatchLockSQL = mustSQL("acquire_alarm_dispatch_lock.sql")
	backlogSnapshotSQL          = mustSQL("backlog_snapshot.sql")
	deleteOrphanEventsSQL       = mustSQL("delete_orphan_events.sql")
	deleteOrphanSendUnitsSQL    = mustSQL("delete_orphan_send_units.sql")
)

// 종료 상태마다 상태와 보존 시각 열을 상수로 쓴 삭제 SQL을 둔다. 상태를 파라미터로 받으면 generic plan이
// 상태별 보존 부분 인덱스를 쓰지 못하고 해당 상태 전체를 읽는다.
var alarmDispatchRetentionDeleteSQL = map[dispatchoutbox.Status]string{
	dispatchoutbox.StatusSent:        mustSQL("alarm_dispatch_retention_delete_sent.sql"),
	dispatchoutbox.StatusDLQ:         mustSQL("alarm_dispatch_retention_delete_dlq.sql"),
	dispatchoutbox.StatusQuarantined: mustSQL("alarm_dispatch_retention_delete_quarantined.sql"),
	dispatchoutbox.StatusCancelled:   mustSQL("alarm_dispatch_retention_delete_cancelled.sql"), //nolint:misspell // 파일명이 실제 상태값 cancelled를 따른다.
}
