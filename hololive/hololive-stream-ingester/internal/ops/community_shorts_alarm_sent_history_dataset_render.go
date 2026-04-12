package ops

import (
	"fmt"
	"strings"
)

func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Alarm Sent History Dataset\n\n")
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
	builder.WriteString("- summary: collected_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CollectedRowCount))
	builder.WriteString("`, duplicates_removed=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateRowCount))
	builder.WriteString("`, sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SentPostCount))
	builder.WriteString("`, community_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CommunitySentPostCount))
	builder.WriteString("`, shorts_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ShortsSentPostCount))
	builder.WriteString("`, baseline_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.BaselinePostCount))
	builder.WriteString("`, matched_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MatchedPostCount))
	builder.WriteString("`, unsent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UnsentPostCount))
	builder.WriteString("`, duplicate_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateSentPostCount))
	builder.WriteString("`, unexpected_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UnexpectedSentPostCount))
	builder.WriteString("`, identifier_mismatch_candidates=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.IdentifierMismatchCandidateCount))
	builder.WriteString("`, verification_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.VerificationRowCount))
	builder.WriteString("`, reference_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ReferenceRowCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SendStatePostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NotSentMissingPostCount))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")

	builder.WriteString("\n## Results\n\n")
	if report.Results.MissingAlarmEvaluated {
		builder.WriteString("- missing alarm aggregation: missing_alarm_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.MissingAlarmPostCount))
		builder.WriteString("`, missing_send_state_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.MissingSendStatePostCount))
		builder.WriteString("`, attempted_missing_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.AttemptedMissingPostCount))
		builder.WriteString("`, not_sent_missing_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.NotSentMissingPostCount))
		builder.WriteString("`\n")
		if report.Results.MissingAlarmZero {
			builder.WriteString("- omission closeout: 누락 0건입니다.\n")
		} else {
			builder.WriteString("- omission closeout: 누락 알람이 `")
			builder.WriteString(fmt.Sprintf("%d", report.Results.MissingAlarmPostCount))
			builder.WriteString("`건 남아 있습니다.\n")
		}
	} else {
		builder.WriteString("- missing alarm aggregation: finalized send-state comparison pending\n")
	}

	if len(report.Results.AlarmTypeComparisons) == 0 {
		builder.WriteString("\n게시물 유형별 대조 결과가 없습니다.\n")
	} else {
		builder.WriteString("\n### By Alarm Type\n\n")
		builder.WriteString("| alarm_type | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |\n")
		builder.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for i := range report.Results.AlarmTypeComparisons {
			row := report.Results.AlarmTypeComparisons[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselinePostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MatchedPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnsentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.DuplicateSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnexpectedSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.IdentifierMismatchCandidateCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MissingAlarmPostCount))
			builder.WriteString(" |\n")
		}
	}

	if len(report.Results.ChannelComparisons) == 0 {
		builder.WriteString("\n채널별 대조 결과가 없습니다.\n")
	} else {
		builder.WriteString("\n### By Channel\n\n")
		builder.WriteString("| channel_id | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |\n")
		builder.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for i := range report.Results.ChannelComparisons {
			row := report.Results.ChannelComparisons[i]
			builder.WriteString("| `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselinePostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MatchedPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnsentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.DuplicateSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnexpectedSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.IdentifierMismatchCandidateCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MissingAlarmPostCount))
			builder.WriteString(" |\n")
		}
	}

	if len(report.MissingAlarmRows) == 0 {
		builder.WriteString("\n누락 알람 게시물이 없습니다.\n")
	} else {
		builder.WriteString("\n## Missing Alarm Rows\n\n")
		builder.WriteString("| missing_reason | send_state | alarm_type | channel_id | channel_post_key | post_key | post_id | actual_published_at | detected_at | state_detected_at | state_alarm_sent_at | verification_verdict | verification_reason | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for i := range report.MissingAlarmRows {
			row := report.MissingAlarmRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.MissingReason))
			builder.WriteString("` | `")
			builder.WriteString(string(row.SendState))
			builder.WriteString("` | `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelPostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.StateDetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.StateAlarmSentAt))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationVerdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationReason))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.VerificationRows) == 0 {
		builder.WriteString("\n검증 verdict row가 없습니다.\n")
	} else {
		builder.WriteString("\n## Verification Rows\n\n")
		builder.WriteString("| verdict | reason | alarm_type | channel_id | post_key | post_id | content_id | baseline_count | sent_count | actual_published_at | detected_at | alarm_sent_at | match_published_at | match_title_hint | match_basis | review_status | related_baseline_post_ids | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for i := range report.VerificationRows {
			row := report.VerificationRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.Verdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.Reason))
			builder.WriteString("` | `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ContentID))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselineCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentCount))
			builder.WriteString(" | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.MatchPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.MatchTitleHint))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.MatchBasis, ", ")))
			builder.WriteString("` | `")
			builder.WriteString(string(row.ReviewStatus))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", ")))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.ReferenceRows) == 0 {
		builder.WriteString("\n정규화된 검증 기준 row가 없습니다.\n")
	} else {
		builder.WriteString("\n## Normalized Verification Reference Rows\n\n")
		builder.WriteString("| alarm_type | channel_id | channel_post_key | post_id | actual_published_at | detected_at | verification_verdict | verification_reason | sent_count | review_status | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |\n")
		for i := range report.ReferenceRows {
			row := report.ReferenceRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelPostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationVerdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationReason))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentCount))
			builder.WriteString(" | `")
			builder.WriteString(string(row.ReviewStatus))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.Rows) == 0 {
		builder.WriteString("\n정규화된 community/shorts sent history row가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n## Normalized Sent History Rows\n\n")
	builder.WriteString("| alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(row.PostKey))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
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
