package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func BuildCommunityShortsDeliveryLogReport(
	query CommunityShortsDeliveryLogQuery,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	generatedAt time.Time,
) CommunityShortsDeliveryLogReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsDeliveryLogQuery(query)

	normalizedRows := make([]CommunityShortsDeliveryLogRow, 0, len(rows))
	postSet := make(map[string]struct{}, len(rows))
	roomSet := make(map[string]struct{}, len(rows))
	summary := CommunityShortsDeliveryLogSummary{}

	for i := range rows {
		row := normalizeCommunityShortsDeliveryLogRow(rows[i])
		row.PublishToEventMillis = durationMillisToEvent(row.ActualPublishedAt, row.EventAt)
		row.DetectToEventMillis = durationMillisToEvent(row.DetectedAt, row.EventAt)
		normalizedRows = append(normalizedRows, row)

		summary.LogCount++
		if strings.EqualFold(strings.TrimSpace(row.SendResult), "success") {
			summary.SuccessLogCount++
		} else {
			summary.FailureLogCount++
		}
		postSet[buildCommunityShortsDeliveryLogPostKey(row)] = struct{}{}
		if roomID := strings.TrimSpace(row.RoomID); roomID != "" {
			roomSet[roomID] = struct{}{}
		}
	}

	summary.UniquePostCount = len(postSet)
	summary.UniqueRoomCount = len(roomSet)

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		leftSortTime := communityShortsDeliveryLogSortTime(normalizedRows[i])
		rightSortTime := communityShortsDeliveryLogSortTime(normalizedRows[j])
		if !leftSortTime.Equal(rightSortTime) {
			return leftSortTime.After(rightSortTime)
		}
		leftPostID := resolveCommunityShortsDeliveryLogPostID(normalizedRows[i])
		rightPostID := resolveCommunityShortsDeliveryLogPostID(normalizedRows[j])
		if leftPostID != rightPostID {
			return leftPostID < rightPostID
		}
		if !normalizedRows[i].EventAt.Equal(normalizedRows[j].EventAt) {
			return normalizedRows[i].EventAt.Before(normalizedRows[j].EventAt)
		}
		return normalizedRows[i].ID < normalizedRows[j].ID
	})

	return CommunityShortsDeliveryLogReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderCommunityShortsDeliveryLogMarkdown(report CommunityShortsDeliveryLogReport) string {
	var builder strings.Builder

	builder.WriteString(buildCommunityShortsDeliveryLogMetadataMarkdown(report))

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 발송 로그가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString(buildCommunityShortsDeliveryLogTableMarkdown(report.Rows))
	return builder.String()
}

func buildCommunityShortsDeliveryLogMetadataMarkdown(report CommunityShortsDeliveryLogReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Delivery Logs Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- mode: `")
	builder.WriteString(string(report.Query.Mode))
	builder.WriteString("`\n")
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
		builder.WriteString("`\n")
	}
	if report.Query.Mode == communityShortsDeliveryLogQueryModeObservation {
		builder.WriteString("- observation runtime: `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
		builder.WriteString("`, cutover: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
		builder.WriteString("`\n")
	}
	builder.WriteString("- summary: logs=`")
	fmt.Fprintf(&builder, "%d", report.Summary.LogCount)
	builder.WriteString("`, success_logs=`")
	fmt.Fprintf(&builder, "%d", report.Summary.SuccessLogCount)
	builder.WriteString("`, failure_logs=`")
	fmt.Fprintf(&builder, "%d", report.Summary.FailureLogCount)
	builder.WriteString("`, unique_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.UniquePostCount)
	builder.WriteString("`, unique_rooms=`")
	fmt.Fprintf(&builder, "%d", report.Summary.UniqueRoomCount)
	builder.WriteString("`, limit=`")
	fmt.Fprintf(&builder, "%d", report.Query.Limit)
	builder.WriteString("`, truncated=`")
	fmt.Fprintf(&builder, "%t", report.Query.Truncated)
	builder.WriteString("`\n")

	return builder.String()
}

func buildCommunityShortsDeliveryLogTableMarkdown(rows []CommunityShortsDeliveryLogRow) string {
	var builder strings.Builder

	builder.WriteString("\n| alarm_type | channel_id | post_id | room_id | attempt | actual_published_at | detected_at | alarm_sent_at | alarm_latency_millis | event_at | publish_to_event_ms | send_result | delivery_path | observation_status | failure_reason |\n")
	builder.WriteString("| --- | --- | --- | --- | ---: | --- | --- | --- | ---: | --- | ---: | --- | --- | --- | --- |\n")
	for i := range rows {
		row := rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.ChannelID)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(resolveCommunityShortsDeliveryLogPostID(row)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.RoomID)))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.AttemptOrdinal)
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.EventAt))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.PublishToEventMillis))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.SendResult)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.DeliveryPath)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.ObservationStatus)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.FailureReason)))
		builder.WriteString("` |\n")
	}

	return builder.String()
}
