package communityshortsops

import (
	"fmt"
	"strconv"
	"strings"
)

func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string {
	var builder strings.Builder

	writeCommunityShortsMarkdownHeading(&builder, 1, "YouTube Community/Shorts Alarm Sent History Dataset")
	writeCommunityShortsAlarmSentHistoryDatasetMetadata(&builder, report)
	writeCommunityShortsAlarmSentHistoryDatasetResults(&builder, report.Results)
	writeCommunityShortsMarkdownSectionTableOrMessage(
		&builder,
		2,
		"Missing Alarm Rows",
		communityShortsAlarmSentHistoryDatasetMissingAlarmColumns,
		buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmMarkdownRows(report.MissingAlarmRows),
		"누락 알람 게시물이 없습니다.",
	)
	writeCommunityShortsMarkdownSectionTableOrMessage(
		&builder,
		2,
		"Verification Rows",
		communityShortsAlarmSentHistoryDatasetVerificationColumns,
		buildCommunityShortsAlarmSentHistoryDatasetVerificationMarkdownRows(report.VerificationRows),
		"검증 verdict row가 없습니다.",
	)
	writeCommunityShortsMarkdownSectionTableOrMessage(
		&builder,
		2,
		"Normalized Verification Reference Rows",
		communityShortsAlarmSentHistoryDatasetReferenceColumns,
		buildCommunityShortsAlarmSentHistoryDatasetReferenceMarkdownRows(report.ReferenceRows),
		"정규화된 검증 기준 row가 없습니다.",
	)
	writeCommunityShortsMarkdownSectionTableOrMessage(
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
	writeCommunityShortsMarkdownKV(builder, "generated at", formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(report.GeneratedAt)))
	writeCommunityShortsMarkdownKV(
		builder,
		"observation runtime",
		formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))+
			", cutover: "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt)),
	)
	writeCommunityShortsMarkdownKV(
		builder,
		"window",
		formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))+
			" -> "+
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd)),
	)
	writeCommunityShortsMarkdownKV(builder, "summary", buildCommunityShortsAlarmSentHistoryDatasetSummaryMarkdown(report.Summary))
}

func writeCommunityShortsAlarmSentHistoryDatasetResults(
	builder *strings.Builder,
	results CommunityShortsAlarmSentHistoryDatasetResults,
) {
	writeCommunityShortsMarkdownHeading(builder, 2, "Results")
	writeCommunityShortsMarkdownKV(builder, "missing alarm aggregation", buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmAggregation(results))
	if results.MissingAlarmEvaluated {
		writeCommunityShortsMarkdownKV(builder, "omission closeout", buildCommunityShortsAlarmSentHistoryDatasetOmissionCloseout(results))
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
	columns []communityShortsMarkdownColumn,
	rows [][]string,
	emptyMessage string,
) {
	if len(rows) == 0 {
		builder.WriteString("\n")
		writeCommunityShortsMarkdownMessage(builder, emptyMessage)
		return
	}
	writeCommunityShortsMarkdownHeading(builder, 3, title)
	writeCommunityShortsMarkdownTable(builder, columns, rows)
}

var communityShortsAlarmSentHistoryDatasetAlarmTypeColumns = []communityShortsMarkdownColumn{
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

var communityShortsAlarmSentHistoryDatasetChannelColumns = []communityShortsMarkdownColumn{
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

var communityShortsAlarmSentHistoryDatasetMissingAlarmColumns = []communityShortsMarkdownColumn{
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

var communityShortsAlarmSentHistoryDatasetVerificationColumns = []communityShortsMarkdownColumn{
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

var communityShortsAlarmSentHistoryDatasetReferenceColumns = []communityShortsMarkdownColumn{
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

var communityShortsAlarmSentHistoryDatasetRowsColumns = []communityShortsMarkdownColumn{
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
		"collected_rows=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.CollectedRowCount)),
		"duplicates_removed=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.DuplicateRowCount)),
		"sent_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.SentPostCount)),
		"community_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.CommunitySentPostCount)),
		"shorts_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.ShortsSentPostCount)),
		"baseline_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.BaselinePostCount)),
		"matched_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.MatchedPostCount)),
		"unsent_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.UnsentPostCount)),
		"duplicate_sent_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.DuplicateSentPostCount)),
		"unexpected_sent_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.UnexpectedSentPostCount)),
		"identifier_mismatch_candidates=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.IdentifierMismatchCandidateCount)),
		"verification_rows=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.VerificationRowCount)),
		"reference_rows=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.ReferenceRowCount)),
		"send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.SendStatePostCount)),
		"missing_alarm_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.MissingAlarmPostCount)),
		"missing_send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.MissingSendStatePostCount)),
		"attempted_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.AttemptedMissingPostCount)),
		"not_sent_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(summary.NotSentMissingPostCount)),
		"earliest_alarm_sent_at=" + formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(summary.EarliestAlarmSentAt)),
		"latest_alarm_sent_at=" + formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(summary.LatestAlarmSentAt)),
	}, ", ")
}

func buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmAggregation(
	results CommunityShortsAlarmSentHistoryDatasetResults,
) string {
	if !results.MissingAlarmEvaluated {
		return "finalized send-state comparison pending"
	}
	return strings.Join([]string{
		"missing_alarm_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(results.MissingAlarmPostCount)),
		"missing_send_state_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(results.MissingSendStatePostCount)),
		"attempted_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(results.AttemptedMissingPostCount)),
		"not_sent_missing_posts=" + formatCommunityShortsMarkdownCode(strconv.Itoa(results.NotSentMissingPostCount)),
	}, ", ")
}

func buildCommunityShortsAlarmSentHistoryDatasetOmissionCloseout(
	results CommunityShortsAlarmSentHistoryDatasetResults,
) string {
	if results.MissingAlarmZero {
		return "누락 0건입니다."
	}
	return "누락 알람이 " + formatCommunityShortsMarkdownCode(strconv.Itoa(results.MissingAlarmPostCount)) + "건 남아 있습니다."
}

func buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeMarkdownRows(
	rows []CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison,
) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
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
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelID)),
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
			formatCommunityShortsMarkdownCode(string(row.MissingReason)),
			formatCommunityShortsMarkdownCode(string(row.SendState)),
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelID)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelPostKey)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostKey)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostID)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.StateDetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.StateAlarmSentAt)),
			formatCommunityShortsMarkdownCode(string(row.VerificationVerdict)),
			formatCommunityShortsMarkdownCode(string(row.VerificationReason)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
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
			formatCommunityShortsMarkdownCode(string(row.Verdict)),
			formatCommunityShortsMarkdownCode(string(row.Reason)),
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelID)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostKey)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostID)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ContentID)),
			strconv.Itoa(row.BaselineCount),
			strconv.Itoa(row.SentCount),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.MatchPublishedAt)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.MatchTitleHint)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(strings.Join(row.MatchBasis, ", "))),
			formatCommunityShortsMarkdownCode(string(row.ReviewStatus)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", "))),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
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
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelID)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.ChannelPostKey)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostID)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(string(row.VerificationVerdict)),
			formatCommunityShortsMarkdownCode(string(row.VerificationReason)),
			strconv.Itoa(row.SentCount),
			formatCommunityShortsMarkdownCode(string(row.ReviewStatus)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
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
			formatCommunityShortsMarkdownCode(string(row.AlarmType)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			formatCommunityShortsMarkdownCode(renderObservationMarkdownCell(row.PostKey)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.PostID)),
			formatCommunityShortsMarkdownCode(fallbackCommunityShortsSendCountValue(row.ContentID)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(row.DetectedAt)),
			formatCommunityShortsMarkdownCode(formatCommunityShortsSendCountTime(row.AlarmSentAt)),
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
