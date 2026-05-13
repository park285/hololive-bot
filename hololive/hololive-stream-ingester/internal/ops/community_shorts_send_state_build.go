package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func BuildCommunityShortsSendStateReport(
	sendStateRows []outbox.PostSendCount,
	query CommunityShortsSendStateQuery,
	generatedAt time.Time,
) CommunityShortsSendStateReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsSendStateQuery(query)

	normalizedRows := make([]CommunityShortsSendStateRow, 0, len(sendStateRows))
	summary := CommunityShortsSendStateSummary{}
	for i := range sendStateRows {
		row := buildCommunityShortsSendStateRow(sendStateRows[i])
		normalizedRows = append(normalizedRows, row)
		accumulateCommunityShortsSendStateSummary(&summary, row)
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		return communityShortsSendStateRowsLess(normalizedRows[i], normalizedRows[j])
	})

	return CommunityShortsSendStateReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderCommunityShortsSendStateMarkdown(report CommunityShortsSendStateReport) string {
	var builder strings.Builder

	builder.WriteString(buildCommunityShortsSendStateMetadataMarkdown(report))

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts per-post send state row가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString(buildCommunityShortsSendStateTableMarkdown(report.Rows))
	return builder.String()
}

func buildCommunityShortsSendStateRow(sendStateRow outbox.PostSendCount) CommunityShortsSendStateRow {
	normalized := normalizeCommunityShortsPostSendCount(sendStateRow)
	alarmSentAt := resolveCommunityShortsSendStateAlarmSentAt(normalized)
	postID := resolveCommunityShortsSendStatePostID(normalized)
	return CommunityShortsSendStateRow{
		PostSendCount:           normalized,
		SendState:               resolveCommunityShortsPerPostSendState(normalized),
		PostKey:                 buildCommunityShortsObservationPostKey(normalized.AlarmType, normalized.ChannelID, postID),
		ReportAlarmType:         normalized.AlarmType,
		ReportChannelID:         normalized.ChannelID,
		ReportPostID:            postID,
		ReportActualPublishedAt: cloneCommunityShortsSendCountTime(normalized.ActualPublishedAt),
		ReportDetectedAt:        cloneCommunityShortsSendCountTime(normalized.DetectedAt),
		ReportAlarmSentAt:       alarmSentAt,
	}
}

func accumulateCommunityShortsSendStateSummary(
	summary *CommunityShortsSendStateSummary,
	row CommunityShortsSendStateRow,
) {
	if summary == nil {
		return
	}
	summary.PostStateCount++
	accumulateCommunityShortsSendStateStatusCounts(summary, row)
	accumulateCommunityShortsSendStateAlarmTypeCounts(summary, row.ReportAlarmType)
	updateCommunityShortsSendStateSummaryTimes(summary, resolveCommunityShortsSendStateObservedAt(row), row.ReportAlarmSentAt)
}

func communityShortsSendStateRowsLess(leftRow, rightRow CommunityShortsSendStateRow) bool {
	left := communityShortsSendStateSortTime(leftRow)
	right := communityShortsSendStateSortTime(rightRow)
	if !left.Equal(right) {
		return left.After(right)
	}
	if leftRow.ReportAlarmType != rightRow.ReportAlarmType {
		return leftRow.ReportAlarmType < rightRow.ReportAlarmType
	}
	if leftRow.ReportChannelID != rightRow.ReportChannelID {
		return leftRow.ReportChannelID < rightRow.ReportChannelID
	}
	if leftRow.ReportPostID != rightRow.ReportPostID {
		return leftRow.ReportPostID < rightRow.ReportPostID
	}
	return leftRow.ContentID < rightRow.ContentID
}

func accumulateCommunityShortsSendStateStatusCounts(summary *CommunityShortsSendStateSummary, row CommunityShortsSendStateRow) {
	switch row.SendState {
	case CommunityShortsPerPostSendStateSent:
		summary.SentPostCount++
	case CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
		summary.AttemptedWithoutSuccessPostCount++
	default:
		summary.NotSentPostCount++
	}
	if row.DuplicateSuccessCount > 0 {
		summary.DuplicateSuccessPostCount++
	}
	if row.FailedAttemptCount > 0 {
		summary.FailedAttemptPostCount++
	}
}

func accumulateCommunityShortsSendStateAlarmTypeCounts(summary *CommunityShortsSendStateSummary, alarmType domain.AlarmType) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		summary.CommunityPostCount++
	case domain.AlarmTypeShorts:
		summary.ShortsPostCount++
	}
}

func updateCommunityShortsSendStateSummaryTimes(
	summary *CommunityShortsSendStateSummary,
	observedAt *time.Time,
	alarmSentAt *time.Time,
) {
	if summary == nil {
		return
	}
	summary.EarliestObservedAt = earlierCommunityShortsSendStateTime(summary.EarliestObservedAt, observedAt)
	summary.LatestObservedAt = laterCommunityShortsSendStateTime(summary.LatestObservedAt, observedAt)
	summary.EarliestAlarmSentAt = earlierCommunityShortsSendStateTime(summary.EarliestAlarmSentAt, alarmSentAt)
	summary.LatestAlarmSentAt = laterCommunityShortsSendStateTime(summary.LatestAlarmSentAt, alarmSentAt)
}

func earlierCommunityShortsSendStateTime(current *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.Before(current.UTC()) {
		return cloneCommunityShortsSendCountTime(candidate)
	}
	return current
}

func laterCommunityShortsSendStateTime(current *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.After(current.UTC()) {
		return cloneCommunityShortsSendCountTime(candidate)
	}
	return current
}

func buildCommunityShortsSendStateMetadataMarkdown(report CommunityShortsSendStateReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Send State Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- finalized: `")
	fmt.Fprintf(&builder, "%t", report.Query.Finalized)
	builder.WriteString("`\n")
	builder.WriteString("- summary: post_states=`")
	fmt.Fprintf(&builder, "%d", report.Summary.PostStateCount)
	builder.WriteString("`, sent_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.SentPostCount)
	builder.WriteString("`, attempted_without_success_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.AttemptedWithoutSuccessPostCount)
	builder.WriteString("`, not_sent_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.NotSentPostCount)
	builder.WriteString("`, duplicate_success_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.DuplicateSuccessPostCount)
	builder.WriteString("`, failed_attempt_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.FailedAttemptPostCount)
	builder.WriteString("`, community_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.CommunityPostCount)
	builder.WriteString("`, shorts_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.ShortsPostCount)
	builder.WriteString("`, earliest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestObservedAt))
	builder.WriteString("`, latest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestObservedAt))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")

	return builder.String()
}

func buildCommunityShortsSendStateTableMarkdown(rows []CommunityShortsSendStateRow) string {
	var builder strings.Builder

	builder.WriteString("\n| send_state | alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at | outbox_count | success_send_count | success_room_count | duplicate_success_count | failed_attempt_count | first_event_at | last_event_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | --- |\n")
	for i := range rows {
		row := rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.SendState))
		builder.WriteString("` | `")
		builder.WriteString(string(row.ReportAlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ReportChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostKey))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ReportPostID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ContentID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportDetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportAlarmSentAt))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.OutboxCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.SuccessSendCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.SuccessRoomCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.DuplicateSuccessCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.FailedAttemptCount)
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.FirstEventAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.LastEventAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}
