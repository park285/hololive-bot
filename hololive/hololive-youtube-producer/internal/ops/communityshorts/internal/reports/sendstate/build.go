package sendstate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func Build(
	sendStateRows []outbox.PostSendCount,
	query Query,
	generatedAt time.Time,
) Report {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeQuery(query)

	normalizedRows := make([]Row, 0, len(sendStateRows))
	summary := Summary{}
	for i := range sendStateRows {
		row := buildRow(&sendStateRows[i])
		normalizedRows = append(normalizedRows, row)
		accumulateSummary(&summary, &row)
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		return rowsLess(&normalizedRows[i], &normalizedRows[j])
	})

	return Report{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderMarkdown(report *Report) string {
	if report == nil {
		return renderMarkdown(&Report{})
	}
	return renderMarkdown(report)
}

func renderMarkdown(report *Report) string {
	var builder strings.Builder

	writeMetadata(&builder, report)

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts per-post send state row가 없습니다.\n")
		return builder.String()
	}

	md.WriteTable(&builder, markdownColumns, buildMarkdownRows(report.Rows))
	return builder.String()
}

func buildRow(sendStateRow *outbox.PostSendCount) Row {
	normalized := normalizePostSendCount(sendStateRow)
	alarmSentAt := resolveAlarmSentAt(&normalized)
	postID := resolvePostID(&normalized)
	return Row{
		PostSendCount:           normalized,
		SendState:               resolvePerPostState(&normalized),
		PostKey:                 buildPostKey(normalized.AlarmType, normalized.ChannelID, postID),
		ReportAlarmType:         normalized.AlarmType,
		ReportChannelID:         normalized.ChannelID,
		ReportPostID:            postID,
		ReportActualPublishedAt: shared.CloneSendCountTime(normalized.ActualPublishedAt),
		ReportDetectedAt:        shared.CloneSendCountTime(normalized.DetectedAt),
		ReportAlarmSentAt:       alarmSentAt,
	}
}

func accumulateSummary(summary *Summary, row *Row) {
	if summary == nil {
		return
	}
	summary.PostStateCount++
	accumulateStatusCounts(summary, row)
	accumulateAlarmTypeCounts(summary, row.ReportAlarmType)
	updateSummaryTimes(summary, resolveObservedAt(row), row.ReportAlarmSentAt)
}

func rowsLess(leftRow, rightRow *Row) bool {
	left := sortTime(leftRow)
	right := sortTime(rightRow)
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

func accumulateStatusCounts(summary *Summary, row *Row) {
	if row == nil {
		return
	}
	accumulateSendStateCount(summary, row.SendState)
	accumulatePositiveCount(&summary.DuplicateSuccessPostCount, row.DuplicateSuccessCount)
	accumulatePositiveCount(&summary.FailedAttemptPostCount, row.FailedAttemptCount)
}

func accumulateSendStateCount(summary *Summary, state PerPostState) {
	switch state {
	case PerPostStateSent:
		summary.SentPostCount++
	case PerPostStateAttemptedWithoutSuccess:
		summary.AttemptedWithoutSuccessPostCount++
	case PerPostStateNotSent:
		summary.NotSentPostCount++
	default:
		summary.NotSentPostCount++
	}
}

func accumulatePositiveCount(counter *int, value int64) {
	if value > 0 {
		(*counter)++
	}
}

func accumulateAlarmTypeCounts(summary *Summary, alarmType domain.AlarmType) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		summary.CommunityPostCount++
	case domain.AlarmTypeShorts:
		summary.ShortsPostCount++
	case domain.AlarmTypeLive, domain.AlarmTypeBirthday, domain.AlarmTypeAnniversary:
		return
	}
}

func updateSummaryTimes(summary *Summary, observedAt, alarmSentAt *time.Time) {
	if summary == nil {
		return
	}
	summary.EarliestObservedAt = earlierTime(summary.EarliestObservedAt, observedAt)
	summary.LatestObservedAt = laterTime(summary.LatestObservedAt, observedAt)
	summary.EarliestAlarmSentAt = earlierTime(summary.EarliestAlarmSentAt, alarmSentAt)
	summary.LatestAlarmSentAt = laterTime(summary.LatestAlarmSentAt, alarmSentAt)
}

func earlierTime(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.Before(current.UTC()) {
		return shared.CloneSendCountTime(candidate)
	}
	return current
}

func laterTime(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.After(current.UTC()) {
		return shared.CloneSendCountTime(candidate)
	}
	return current
}

func writeMetadata(builder *strings.Builder, report *Report) {
	md.WriteHeading(builder, 1, "YouTube Community/Shorts Send State Report")
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(shared.FallbackSendCountValue(report.Query.ObservationRuntimeName))+
			", cutover: "+
			md.Code(shared.FormatSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(shared.FormatSendCountTimePtr(report.Query.WindowStart))+
			" -> "+
			md.Code(shared.FormatSendCountTimePtr(report.Query.WindowEnd)),
	)
	md.WriteKV(builder, "finalized", md.Code(fmt.Sprintf("%t", report.Query.Finalized)))
	md.WriteKV(builder, "summary", buildSummaryMarkdown(&report.Summary))
}

func buildSummaryMarkdown(summary *Summary) string {
	if summary == nil {
		return ""
	}
	parts := []string{
		"post_states=" + md.Code(fmt.Sprintf("%d", summary.PostStateCount)),
		"sent_posts=" + md.Code(fmt.Sprintf("%d", summary.SentPostCount)),
		"attempted_without_success_posts=" + md.Code(fmt.Sprintf("%d", summary.AttemptedWithoutSuccessPostCount)),
		"not_sent_posts=" + md.Code(fmt.Sprintf("%d", summary.NotSentPostCount)),
		"duplicate_success_posts=" + md.Code(fmt.Sprintf("%d", summary.DuplicateSuccessPostCount)),
		"failed_attempt_posts=" + md.Code(fmt.Sprintf("%d", summary.FailedAttemptPostCount)),
		"community_posts=" + md.Code(fmt.Sprintf("%d", summary.CommunityPostCount)),
		"shorts_posts=" + md.Code(fmt.Sprintf("%d", summary.ShortsPostCount)),
		"earliest_observed_at=" + md.Code(shared.FormatSendCountTimePtr(summary.EarliestObservedAt)),
		"latest_observed_at=" + md.Code(shared.FormatSendCountTimePtr(summary.LatestObservedAt)),
		"earliest_alarm_sent_at=" + md.Code(shared.FormatSendCountTimePtr(summary.EarliestAlarmSentAt)),
		"latest_alarm_sent_at=" + md.Code(shared.FormatSendCountTimePtr(summary.LatestAlarmSentAt)),
	}
	return strings.Join(parts, ", ")
}

var markdownColumns = []md.Column{
	{Header: "send_state"},
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_key"},
	{Header: "post_id"},
	{Header: "content_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
	{Header: "outbox_count", AlignRight: true},
	{Header: "success_send_count", AlignRight: true},
	{Header: "success_room_count", AlignRight: true},
	{Header: "duplicate_success_count", AlignRight: true},
	{Header: "failed_attempt_count", AlignRight: true},
	{Header: "first_event_at"},
	{Header: "last_event_at"},
}

func buildMarkdownRows(rows []Row) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.SendState)),
			md.Code(string(row.ReportAlarmType)),
			md.Code(shared.FallbackSendCountValue(row.ReportChannelID)),
			md.Code(shared.FallbackSendCountValue(row.PostKey)),
			md.Code(shared.FallbackSendCountValue(row.ReportPostID)),
			md.Code(shared.FallbackSendCountValue(row.ContentID)),
			md.Code(shared.FormatSendCountTimePtr(row.ReportActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.ReportDetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.ReportAlarmSentAt)),
			fmt.Sprintf("%d", row.OutboxCount),
			fmt.Sprintf("%d", row.SuccessSendCount),
			fmt.Sprintf("%d", row.SuccessRoomCount),
			fmt.Sprintf("%d", row.DuplicateSuccessCount),
			fmt.Sprintf("%d", row.FailedAttemptCount),
			md.Code(shared.FormatSendCountTimePtr(row.FirstEventAt)),
			md.Code(shared.FormatSendCountTimePtr(row.LastEventAt)),
		})
	}
	return markdownRows
}
