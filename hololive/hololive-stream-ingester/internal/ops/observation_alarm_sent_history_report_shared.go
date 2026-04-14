package ops

import (
	"fmt"
	"strings"
	"time"

	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type observationAlarmSentHistoryQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type observationAlarmSentHistorySummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

func buildObservationAlarmSentHistorySummary(finalized observationAlarmSentHistoryFinalizationResult) observationAlarmSentHistorySummary {
	summary := observationAlarmSentHistorySummary{
		CollectedRowCount: finalized.CollectedRowCount,
		DuplicateRowCount: finalized.DuplicateRowCount,
	}
	for i := range finalized.Rows {
		row := finalized.Rows[i]
		summary.SentPostCount++
		if summary.EarliestAlarmSentAt == nil || row.AlarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
			summary.EarliestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
		if summary.LatestAlarmSentAt == nil || row.AlarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
			summary.LatestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
	}
	return summary
}

func renderObservationAlarmSentHistoryMarkdown(
	title string,
	generatedAt time.Time,
	query observationAlarmSentHistoryQuery,
	summary observationAlarmSentHistorySummary,
	comparison trackingrepo.ObservationPostComparisonResult,
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	emptyMessage string,
) string {
	var builder strings.Builder

	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(generatedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(query.ObservationRuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(query.ObservationBigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(query.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(query.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- summary: collected_rows=`")
	fmt.Fprintf(&builder, "%d", summary.CollectedRowCount)
	builder.WriteString("`, duplicates_removed=`")
	fmt.Fprintf(&builder, "%d", summary.DuplicateRowCount)
	builder.WriteString("`, sent_posts=`")
	fmt.Fprintf(&builder, "%d", summary.SentPostCount)
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(summary.LatestAlarmSentAt))
	builder.WriteString("`\n")
	builder.WriteString(renderObservationPostComparisonSummaryMarkdown(comparison))
	builder.WriteString(renderObservationPostComparisonVerdictsMarkdown(comparison))
	builder.WriteString(renderObservationIdentifierMismatchCandidatesMarkdown(comparison))

	if len(rows) == 0 {
		builder.WriteString("\n")
		builder.WriteString(emptyMessage)
		builder.WriteString("\n")
		return builder.String()
	}

	builder.WriteString("\n| post_id | channel_id | content_id | actual_published_at | detected_at | alarm_sent_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for i := range rows {
		row := rows[i]
		builder.WriteString("| `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ContentID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.AlarmSentAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func normalizeObservationAlarmSentHistoryCollectQuery(
	runtimeName string,
	cutoverAt *time.Time,
) (observationAlarmSentHistoryQuery, error) {
	trimmedRuntimeName := strings.TrimSpace(runtimeName)
	normalizedCutoverAt := cloneCommunityShortsSendCountTime(cutoverAt)
	if trimmedRuntimeName == "" || normalizedCutoverAt == nil || normalizedCutoverAt.IsZero() {
		return observationAlarmSentHistoryQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}
	return observationAlarmSentHistoryQuery{
		ObservationRuntimeName:      trimmedRuntimeName,
		ObservationBigBangCutoverAt: normalizedCutoverAt,
	}, nil
}

func normalizeObservationAlarmSentHistoryQuery(query observationAlarmSentHistoryQuery) observationAlarmSentHistoryQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}
