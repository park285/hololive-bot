package majorevent

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	getExistingExternalIDsSQL      = mustSQL("get_existing_external_ids.sql")
	getSubscribedRoomsSQL          = mustSQL("get_subscribed_rooms.sql")
	isSubscribedSQL                = mustSQL("is_subscribed.sql")
	majorEventSelectColumnsSQL     = mustSQL("major_event_select_columns.sql")
	markEventsAsMonthlyNotifiedSQL = mustSQL("mark_events_as_monthly_notified.sql")
	markEventsAsNotifiedSQL        = mustSQL("mark_events_as_notified.sql")
	subscribeSQL                   = mustSQL("subscribe.sql")
	unsubscribeSQL                 = mustSQL("unsubscribe.sql")
	updateEventLinkStatusesSQL     = mustSQL("update_event_link_statuses.sql")
	updateExpiredEventsSQL         = mustSQL("update_expired_events.sql")
	upsertEventSQL                 = mustSQL("upsert_event.sql")
)

// 같은 SELECT 머리에 고정 조건만 붙이는 조회 문장은 초기화 때 완성해 호출마다 문자열을 잇지 않는다.
var (
	getEventsByDateRangeSQL = majorEventSelectColumnsSQL + `
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'Asia/Seoul')::date) <= $5
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'Asia/Seoul')::date) >= $4
		  AND (notified_week IS NULL OR notified_week != $6)
		ORDER BY event_start_date ASC
	`
	getEventsByMonthSQL = majorEventSelectColumnsSQL + `
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'Asia/Seoul')::date) <= $4
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'Asia/Seoul')::date) >= $5
		  AND (notified_month IS NULL OR notified_month != $6)
		ORDER BY event_start_date ASC
	`
	getAllActiveEventsSQL = majorEventSelectColumnsSQL + `
		WHERE status = $1
		ORDER BY event_start_date ASC
	`
)
