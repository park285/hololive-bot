package reports

import (
	"strconv"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

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
