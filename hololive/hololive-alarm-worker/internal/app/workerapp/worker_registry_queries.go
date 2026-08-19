package workerapp

import _ "embed"

//go:embed queries/alarm_dispatch_ready_snapshot.sql
var alarmDispatchReadySnapshotSQL string

//go:embed queries/notification_delivery_ready_snapshot.sql
var notificationDeliveryReadySnapshotSQL string

//go:embed queries/youtube_delivery_ready_snapshot.sql
var youtubeDeliveryReadySnapshotSQL string
