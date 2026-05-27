package deliverylogs

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func Build(
	query Query,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	generatedAt time.Time,
) Report {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeQuery(query)

	normalizedRows, summary := buildRows(rows)
	sort.SliceStable(normalizedRows, func(i, j int) bool {
		return rowLess(normalizedRows[i], normalizedRows[j])
	})

	return Report{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func buildRows(rows []domain.YouTubeNotificationDeliveryTelemetry) ([]Row, Summary) {
	normalizedRows := make([]Row, 0, len(rows))
	postSet := make(map[string]struct{}, len(rows))
	roomSet := make(map[string]struct{}, len(rows))
	summary := Summary{}

	for i := range rows {
		row := normalizeRow(rows[i])
		row.PublishToEventMillis = durationMillisToEvent(row.ActualPublishedAt, row.EventAt)
		row.DetectToEventMillis = durationMillisToEvent(row.DetectedAt, row.EventAt)
		normalizedRows = append(normalizedRows, row)

		summary.LogCount++
		if strings.EqualFold(strings.TrimSpace(row.SendResult), "success") {
			summary.SuccessLogCount++
		} else {
			summary.FailureLogCount++
		}
		postSet[buildPostKey(row)] = struct{}{}
		if roomID := strings.TrimSpace(row.RoomID); roomID != "" {
			roomSet[roomID] = struct{}{}
		}
	}

	summary.UniquePostCount = len(postSet)
	summary.UniqueRoomCount = len(roomSet)
	return normalizedRows, summary
}

func rowLess(left Row, right Row) bool {
	leftSortTime := rowSortTime(left)
	rightSortTime := rowSortTime(right)
	if !leftSortTime.Equal(rightSortTime) {
		return leftSortTime.After(rightSortTime)
	}
	leftPostID := resolvePostID(left)
	rightPostID := resolvePostID(right)
	if leftPostID != rightPostID {
		return leftPostID < rightPostID
	}
	if !left.EventAt.Equal(right.EventAt) {
		return left.EventAt.Before(right.EventAt)
	}
	return left.ID < right.ID
}

func RenderMarkdown(report Report) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Delivery Logs Report")
	md.WriteKV(&builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(&builder, "mode", md.Code(string(report.Query.Mode)))
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		md.WriteKV(
			&builder,
			"window",
			md.Code(shared.FormatSendCountTimePtr(report.Query.WindowStart))+
				" -> "+
				md.Code(shared.FormatSendCountTimePtr(report.Query.WindowEnd)),
		)
	}
	if report.Query.Mode == QueryModeObservation {
		md.WriteKV(
			&builder,
			"observation runtime",
			md.Code(shared.FallbackSendCountValue(report.Query.ObservationRuntimeName))+
				", cutover: "+
				md.Code(shared.FormatSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
		)
	}
	md.WriteKV(&builder, "summary", buildSummaryMarkdown(report.Summary, report.Query))

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 발송 로그가 없습니다.\n")
		return builder.String()
	}

	md.WriteTable(&builder, markdownColumns, buildMarkdownRows(report.Rows))
	return builder.String()
}

func buildSummaryMarkdown(summary Summary, query Query) string {
	parts := []string{
		"logs=" + md.Code(fmt.Sprintf("%d", summary.LogCount)),
		"success_logs=" + md.Code(fmt.Sprintf("%d", summary.SuccessLogCount)),
		"failure_logs=" + md.Code(fmt.Sprintf("%d", summary.FailureLogCount)),
		"unique_posts=" + md.Code(fmt.Sprintf("%d", summary.UniquePostCount)),
		"unique_rooms=" + md.Code(fmt.Sprintf("%d", summary.UniqueRoomCount)),
		"limit=" + md.Code(fmt.Sprintf("%d", query.Limit)),
		"truncated=" + md.Code(fmt.Sprintf("%t", query.Truncated)),
	}
	return strings.Join(parts, ", ")
}

var markdownColumns = []md.Column{
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_id"},
	{Header: "room_id"},
	{Header: "attempt", AlignRight: true},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
	{Header: "alarm_latency_millis", AlignRight: true},
	{Header: "event_at"},
	{Header: "publish_to_event_ms", AlignRight: true},
	{Header: "send_result"},
	{Header: "delivery_path"},
	{Header: "observation_status"},
	{Header: "failure_reason"},
}

func buildMarkdownRows(rows []Row) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.ChannelID))),
			md.Code(shared.FallbackSendCountValue(resolvePostID(row))),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.RoomID))),
			fmt.Sprintf("%d", row.AttemptOrdinal),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.DetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.AlarmSentAt)),
			shared.FormatSendCountInt64Ptr(row.AlarmLatencyMillis),
			md.Code(shared.FormatSendCountTime(row.EventAt)),
			shared.FormatSendCountInt64Ptr(row.PublishToEventMillis),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.SendResult))),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.DeliveryPath))),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.ObservationStatus))),
			md.Code(shared.FallbackSendCountValue(strings.TrimSpace(row.FailureReason))),
		})
	}
	return markdownRows
}
