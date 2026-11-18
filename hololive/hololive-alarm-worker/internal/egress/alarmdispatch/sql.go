package alarmdispatch

import (
	"embed"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	acquireAlarmDispatchLockSQL = mustSQL("acquire_alarm_dispatch_lock.sql")
	backlogSnapshotSQL          = mustSQL("backlog_snapshot.sql")
	deleteOrphanEventsSQL       = mustSQL("delete_orphan_events.sql")
	deleteOrphanSendUnitsSQL    = mustSQL("delete_orphan_send_units.sql")
)

var alarmDispatchRetentionDeleteSQL = map[dispatchoutbox.Status]string{
	dispatchoutbox.StatusSent:        mustSQL("alarm_dispatch_retention_delete_sent.sql"),
	dispatchoutbox.StatusDLQ:         mustSQL("alarm_dispatch_retention_delete_dlq.sql"),
	dispatchoutbox.StatusQuarantined: mustSQL("alarm_dispatch_retention_delete_quarantined.sql"),
	dispatchoutbox.StatusCancelled:   mustSQL("alarm_dispatch_retention_delete_cancelled.sql"), //nolint:misspell // 파일명이 실제 상태값 cancelled를 따른다.
}
