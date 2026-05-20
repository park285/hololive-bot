package reports

import (
	"fmt"
	"strconv"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, "YouTube Community/Shorts Alarm Sent History Dataset")
	writeCommunityShortsAlarmSentHistoryDatasetMetadata(&builder, report)
	writeCommunityShortsAlarmSentHistoryDatasetResults(&builder, report.Results)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Missing Alarm Rows",
		communityShortsAlarmSentHistoryDatasetMissingAlarmColumns,
		buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmMarkdownRows(report.MissingAlarmRows),
		"누락 알람 게시물이 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Verification Rows",
		communityShortsAlarmSentHistoryDatasetVerificationColumns,
		buildCommunityShortsAlarmSentHistoryDatasetVerificationMarkdownRows(report.VerificationRows),
		"검증 verdict row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Verification Reference Rows",
		communityShortsAlarmSentHistoryDatasetReferenceColumns,
		buildCommunityShortsAlarmSentHistoryDatasetReferenceMarkdownRows(report.ReferenceRows),
		"정규화된 검증 기준 row가 없습니다.",
	)
	md.WriteSectionTableOrMessage(
		&builder,
		2,
		"Normalized Sent History Rows",
		communityShortsAlarmSentHistoryDatasetRowsColumns,
		buildCommunityShortsAlarmSentHistoryDatasetMarkdownRows(report.Rows),
		"정규화된 community/shorts sent history row가 없습니다.",
	)
	return builder.String()
}

func writeCommunityShortsAlarmSentHistoryDatasetMetadata(
	builder *strings.Builder,
	report CommunityShortsAlarmSentHistoryDatasetReport,
) {
	md.WriteKV(builder, "generated at", md.Code(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
			", cutover: "+
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
			" -> "+
			md.Code(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
	)
	md.WriteKV(builder, "summary", buildCommunityShortsAlarmSentHistoryDatasetSummaryMarkdown(report.Summary))
}

func writeCommunityShortsAlarmSentHistoryDatasetResults(
	builder *strings.Builder,
	results CommunityShortsAlarmSentHistoryDatasetResults,
) {
	md.WriteHeading(builder, 2, "Results")
	md.WriteKV(builder, "missing alarm aggregation", buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmAggregation(results))
	if results.MissingAlarmEvaluated {
		md.WriteKV(builder, "omission closeout", buildCommunityShortsAlarmSentHistoryDatasetOmissionCloseout(results))
	}
	writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
		builder,
		"By Alarm Type",
		communityShortsAlarmSentHistoryDatasetAlarmTypeColumns,
		buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeMarkdownRows(results.AlarmTypeComparisons),
		"게시물 유형별 대조 결과가 없습니다.",
	)
	writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
		builder,
		"By Channel",
		communityShortsAlarmSentHistoryDatasetChannelColumns,
		buildCommunityShortsAlarmSentHistoryDatasetChannelMarkdownRows(results.ChannelComparisons),
		"채널별 대조 결과가 없습니다.",
	)
}

func writeCommunityShortsAlarmSentHistoryDatasetComparisonSection(
	builder *strings.Builder,
	title string,
	columns []md.Column,
	rows [][]string,
	emptyMessage string,
) {
	if len(rows) == 0 {
		builder.WriteString("\n")
		md.WriteMessage(builder, emptyMessage)
		return
	}
	md.WriteHeading(builder, 3, title)
	md.WriteTable(builder, columns, rows)
}

var communityShortsAlarmSentHistoryDatasetAlarmTypeColumns = []md.Column{
	{Header: "alarm_type"},
	{Header: "baseline_posts", AlignRight: true},
	{Header: "sent_posts", AlignRight: true},
	{Header: "matched_posts", AlignRight: true},
	{Header: "unsent_posts", AlignRight: true},
	{Header: "duplicate_sent_posts", AlignRight: true},
	{Header: "unexpected_sent_posts", AlignRight: true},
	{Header: "identifier_mismatch_candidates", AlignRight: true},
	{Header: "missing_alarm_posts", AlignRight: true},
}

var communityShortsAlarmSentHistoryDatasetChannelColumns = []md.Column{
	{Header: "channel_id"},
	{Header: "baseline_posts", AlignRight: true},
	{Header: "sent_posts", AlignRight: true},
	{Header: "matched_posts", AlignRight: true},
	{Header: "unsent_posts", AlignRight: true},
	{Header: "duplicate_sent_posts", AlignRight: true},
	{Header: "unexpected_sent_posts", AlignRight: true},
	{Header: "identifier_mismatch_candidates", AlignRight: true},
	{Header: "missing_alarm_posts", AlignRight: true},
}

var communityShortsAlarmSentHistoryDatasetMissingAlarmColumns = []md.Column{
	{Header: "missing_reason"},
	{Header: "send_state"},
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "channel_post_key"},
	{Header: "post_key"},
	{Header: "post_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "state_detected_at"},
	{Header: "state_alarm_sent_at"},
	{Header: "verification_verdict"},
	{Header: "verification_reason"},
	{Header: "related_sent_post_ids"},
}

var communityShortsAlarmSentHistoryDatasetVerificationColumns = []md.Column{
	{Header: "verdict"},
	{Header: "reason"},
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_key"},
	{Header: "post_id"},
	{Header: "content_id"},
	{Header: "baseline_count", AlignRight: true},
	{Header: "sent_count", AlignRight: true},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
	{Header: "match_published_at"},
	{Header: "match_title_hint"},
	{Header: "match_basis"},
	{Header: "review_status"},
	{Header: "related_baseline_post_ids"},
	{Header: "related_sent_post_ids"},
}

var communityShortsAlarmSentHistoryDatasetReferenceColumns = []md.Column{
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "channel_post_key"},
	{Header: "post_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "verification_verdict"},
	{Header: "verification_reason"},
	{Header: "sent_count", AlignRight: true},
	{Header: "review_status"},
	{Header: "related_sent_post_ids"},
}

var communityShortsAlarmSentHistoryDatasetRowsColumns = []md.Column{
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_key"},
	{Header: "post_id"},
	{Header: "content_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
}

func buildCommunityShortsAlarmSentHistoryDatasetSummaryMarkdown(
	summary CommunityShortsAlarmSentHistoryDatasetSummary,
) string {
	return strings.Join([]string{
		"collected_rows=" + md.Code(strconv.Itoa(summary.CollectedRowCount)),
		"duplicates_removed=" + md.Code(strconv.Itoa(summary.DuplicateRowCount)),
		"sent_posts=" + md.Code(strconv.Itoa(summary.SentPostCount)),
		"community_posts=" + md.Code(strconv.Itoa(summary.CommunitySentPostCount)),
		"shorts_posts=" + md.Code(strconv.Itoa(summary.ShortsSentPostCount)),
		"baseline_posts=" + md.Code(strconv.Itoa(summary.BaselinePostCount)),
		"matched_posts=" + md.Code(strconv.Itoa(summary.MatchedPostCount)),
		"unsent_posts=" + md.Code(strconv.Itoa(summary.UnsentPostCount)),
		"duplicate_sent_posts=" + md.Code(strconv.Itoa(summary.DuplicateSentPostCount)),
		"unexpected_sent_posts=" + md.Code(strconv.Itoa(summary.UnexpectedSentPostCount)),
		"identifier_mismatch_candidates=" + md.Code(strconv.Itoa(summary.IdentifierMismatchCandidateCount)),
		"verification_rows=" + md.Code(strconv.Itoa(summary.VerificationRowCount)),
		"reference_rows=" + md.Code(strconv.Itoa(summary.ReferenceRowCount)),
		"send_state_posts=" + md.Code(strconv.Itoa(summary.SendStatePostCount)),
		"missing_alarm_posts=" + md.Code(strconv.Itoa(summary.MissingAlarmPostCount)),
		"missing_send_state_posts=" + md.Code(strconv.Itoa(summary.MissingSendStatePostCount)),
		"attempted_missing_posts=" + md.Code(strconv.Itoa(summary.AttemptedMissingPostCount)),
		"not_sent_missing_posts=" + md.Code(strconv.Itoa(summary.NotSentMissingPostCount)),
		"earliest_alarm_sent_at=" + md.Code(formatCommunityShortsSendCountTimePtr(summary.EarliestAlarmSentAt)),
		"latest_alarm_sent_at=" + md.Code(formatCommunityShortsSendCountTimePtr(summary.LatestAlarmSentAt)),
	}, ", ")
}

func buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmAggregation(
	results CommunityShortsAlarmSentHistoryDatasetResults,
) string {
	if !results.MissingAlarmEvaluated {
		return "finalized send-state comparison pending"
	}
	return strings.Join([]string{
		"missing_alarm_posts=" + md.Code(strconv.Itoa(results.MissingAlarmPostCount)),
		"missing_send_state_posts=" + md.Code(strconv.Itoa(results.MissingSendStatePostCount)),
		"attempted_missing_posts=" + md.Code(strconv.Itoa(results.AttemptedMissingPostCount)),
		"not_sent_missing_posts=" + md.Code(strconv.Itoa(results.NotSentMissingPostCount)),
	}, ", ")
}

func buildCommunityShortsAlarmSentHistoryDatasetOmissionCloseout(
	results CommunityShortsAlarmSentHistoryDatasetResults,
) string {
	if results.MissingAlarmZero {
		return "누락 0건입니다."
	}
	return "누락 알람이 " + md.Code(strconv.Itoa(results.MissingAlarmPostCount)) + "건 남아 있습니다."
}

func buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			strconv.Itoa(row.BaselinePostCount),
			strconv.Itoa(row.SentPostCount),
			strconv.Itoa(row.MatchedPostCount),
			strconv.Itoa(row.UnsentPostCount),
			strconv.Itoa(row.DuplicateSentPostCount),
			strconv.Itoa(row.UnexpectedSentPostCount),
			strconv.Itoa(row.IdentifierMismatchCandidateCount),
			strconv.Itoa(row.MissingAlarmPostCount),
		})
	}
	return markdownRows
}

func buildCommunityShortsAlarmSentHistoryDatasetChannelMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetChannelComparison,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(renderObservationMarkdownCell(row.ChannelID)),
			strconv.Itoa(row.BaselinePostCount),
			strconv.Itoa(row.SentPostCount),
			strconv.Itoa(row.MatchedPostCount),
			strconv.Itoa(row.UnsentPostCount),
			strconv.Itoa(row.DuplicateSentPostCount),
			strconv.Itoa(row.UnexpectedSentPostCount),
			strconv.Itoa(row.IdentifierMismatchCandidateCount),
			strconv.Itoa(row.MissingAlarmPostCount),
		})
	}
	return markdownRows
}

func buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.MissingReason)),
			md.Code(string(row.SendState)),
			md.Code(string(row.AlarmType)),
			md.Code(renderObservationMarkdownCell(row.ChannelID)),
			md.Code(renderObservationMarkdownCell(row.ChannelPostKey)),
			md.Code(renderObservationMarkdownCell(row.PostKey)),
			md.Code(renderObservationMarkdownCell(row.PostID)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.StateDetectedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.StateAlarmSentAt)),
			md.Code(string(row.VerificationVerdict)),
			md.Code(string(row.VerificationReason)),
			md.Code(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildCommunityShortsAlarmSentHistoryDatasetVerificationMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetVerificationRow,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.Verdict)),
			md.Code(string(row.Reason)),
			md.Code(string(row.AlarmType)),
			md.Code(renderObservationMarkdownCell(row.ChannelID)),
			md.Code(renderObservationMarkdownCell(row.PostKey)),
			md.Code(renderObservationMarkdownCell(row.PostID)),
			md.Code(renderObservationMarkdownCell(row.ContentID)),
			strconv.Itoa(row.BaselineCount),
			strconv.Itoa(row.SentCount),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.MatchPublishedAt)),
			md.Code(renderObservationMarkdownCell(row.MatchTitleHint)),
			md.Code(renderObservationMarkdownCell(strings.Join(row.MatchBasis, ", "))),
			md.Code(string(row.ReviewStatus)),
			md.Code(renderObservationMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", "))),
			md.Code(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildCommunityShortsAlarmSentHistoryDatasetReferenceMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(renderObservationMarkdownCell(row.ChannelID)),
			md.Code(renderObservationMarkdownCell(row.ChannelPostKey)),
			md.Code(renderObservationMarkdownCell(row.PostID)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			md.Code(string(row.VerificationVerdict)),
			md.Code(string(row.VerificationReason)),
			strconv.Itoa(row.SentCount),
			md.Code(string(row.ReviewStatus)),
			md.Code(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildCommunityShortsAlarmSentHistoryDatasetMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetRow,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			md.Code(renderObservationMarkdownCell(row.PostKey)),
			md.Code(fallbackCommunityShortsSendCountValue(row.PostID)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ContentID)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTime(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTime(row.AlarmSentAt)),
		})
	}
	return markdownRows
}

func normalizeCommunityShortsAlarmSentHistoryDatasetCollectOptions(
	options CommunityShortsAlarmSentHistoryDatasetCollectOptions,
) (CommunityShortsAlarmSentHistoryDatasetQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return CommunityShortsAlarmSentHistoryDatasetQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return CommunityShortsAlarmSentHistoryDatasetQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeCommunityShortsAlarmSentHistoryDatasetQuery(
	query CommunityShortsAlarmSentHistoryDatasetQuery,
) CommunityShortsAlarmSentHistoryDatasetQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}
