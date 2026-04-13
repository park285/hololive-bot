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
		normalized := normalizeCommunityShortsPostSendCount(sendStateRows[i])
		alarmSentAt := resolveCommunityShortsSendStateAlarmSentAt(normalized)
		postID := resolveCommunityShortsSendStatePostID(normalized)
		row := CommunityShortsSendStateRow{
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
		normalizedRows = append(normalizedRows, row)

		summary.PostStateCount++
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
		switch row.ReportAlarmType {
		case domain.AlarmTypeCommunity:
			summary.CommunityPostCount++
		case domain.AlarmTypeShorts:
			summary.ShortsPostCount++
		}
		if observedAt := resolveCommunityShortsSendStateObservedAt(row); observedAt != nil {
			if summary.EarliestObservedAt == nil || observedAt.Before(summary.EarliestObservedAt.UTC()) {
				summary.EarliestObservedAt = cloneCommunityShortsSendCountTime(observedAt)
			}
			if summary.LatestObservedAt == nil || observedAt.After(summary.LatestObservedAt.UTC()) {
				summary.LatestObservedAt = cloneCommunityShortsSendCountTime(observedAt)
			}
		}
		if alarmSentAt != nil {
			if summary.EarliestAlarmSentAt == nil || alarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
				summary.EarliestAlarmSentAt = cloneCommunityShortsSendCountTime(alarmSentAt)
			}
			if summary.LatestAlarmSentAt == nil || alarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
				summary.LatestAlarmSentAt = cloneCommunityShortsSendCountTime(alarmSentAt)
			}
		}
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := communityShortsSendStateSortTime(normalizedRows[i])
		right := communityShortsSendStateSortTime(normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if normalizedRows[i].ReportAlarmType != normalizedRows[j].ReportAlarmType {
			return normalizedRows[i].ReportAlarmType < normalizedRows[j].ReportAlarmType
		}
		if normalizedRows[i].ReportChannelID != normalizedRows[j].ReportChannelID {
			return normalizedRows[i].ReportChannelID < normalizedRows[j].ReportChannelID
		}
		if normalizedRows[i].ReportPostID != normalizedRows[j].ReportPostID {
			return normalizedRows[i].ReportPostID < normalizedRows[j].ReportPostID
		}
		return normalizedRows[i].ContentID < normalizedRows[j].ContentID
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
	builder.WriteString(fmt.Sprintf("%t", report.Query.Finalized))
	builder.WriteString("`\n")
	builder.WriteString("- summary: post_states=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.PostStateCount))
	builder.WriteString("`, sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SentPostCount))
	builder.WriteString("`, attempted_without_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.AttemptedWithoutSuccessPostCount))
	builder.WriteString("`, not_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NotSentPostCount))
	builder.WriteString("`, duplicate_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateSuccessPostCount))
	builder.WriteString("`, failed_attempt_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.FailedAttemptPostCount))
	builder.WriteString("`, community_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CommunityPostCount))
	builder.WriteString("`, shorts_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ShortsPostCount))
	builder.WriteString("`, earliest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestObservedAt))
	builder.WriteString("`, latest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestObservedAt))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts per-post send state row가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| send_state | alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at | outbox_count | success_send_count | success_room_count | duplicate_success_count | failed_attempt_count | first_event_at | last_event_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
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
		builder.WriteString(fmt.Sprintf("%d", row.OutboxCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessSendCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessRoomCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.DuplicateSuccessCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.FailedAttemptCount))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.FirstEventAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.LastEventAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}
