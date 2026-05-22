package reports

import (
	"strconv"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

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
