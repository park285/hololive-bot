package alarmhistory

import (
	"strconv"
	"strings"

	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

var alarmTypeColumns = []md.Column{
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

var channelColumns = []md.Column{
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

var missingAlarmColumns = []md.Column{
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

var verificationColumns = []md.Column{
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

var referenceColumns = []md.Column{
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

var rowsColumns = []md.Column{
	{Header: "alarm_type"},
	{Header: "channel_id"},
	{Header: "post_key"},
	{Header: "post_id"},
	{Header: "content_id"},
	{Header: "actual_published_at"},
	{Header: "detected_at"},
	{Header: "alarm_sent_at"},
}

func buildAlarmTypeMarkdownRows(rows []DatasetAlarmTypeComparison) [][]string {
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

func buildChannelMarkdownRows(rows []DatasetChannelComparison) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(renderMarkdownCell(row.ChannelID)),
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

func buildMissingAlarmMarkdownRows(rows []DatasetMissingAlarmRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.MissingReason)),
			md.Code(string(row.SendState)),
			md.Code(string(row.AlarmType)),
			md.Code(renderMarkdownCell(row.ChannelID)),
			md.Code(renderMarkdownCell(row.ChannelPostKey)),
			md.Code(renderMarkdownCell(row.PostKey)),
			md.Code(renderMarkdownCell(row.PostID)),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.DetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.StateDetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.StateAlarmSentAt)),
			md.Code(string(row.VerificationVerdict)),
			md.Code(string(row.VerificationReason)),
			md.Code(renderMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildVerificationMarkdownRows(rows []DatasetVerificationRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.Verdict)),
			md.Code(string(row.Reason)),
			md.Code(string(row.AlarmType)),
			md.Code(renderMarkdownCell(row.ChannelID)),
			md.Code(renderMarkdownCell(row.PostKey)),
			md.Code(renderMarkdownCell(row.PostID)),
			md.Code(renderMarkdownCell(row.ContentID)),
			strconv.Itoa(row.BaselineCount),
			strconv.Itoa(row.SentCount),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.DetectedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.AlarmSentAt)),
			md.Code(shared.FormatSendCountTimePtr(row.MatchPublishedAt)),
			md.Code(renderMarkdownCell(row.MatchTitleHint)),
			md.Code(renderMarkdownCell(strings.Join(row.MatchBasis, ", "))),
			md.Code(string(row.ReviewStatus)),
			md.Code(renderMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", "))),
			md.Code(renderMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildReferenceMarkdownRows(rows []DatasetReferenceRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(renderMarkdownCell(row.ChannelID)),
			md.Code(renderMarkdownCell(row.ChannelPostKey)),
			md.Code(renderMarkdownCell(row.PostID)),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTimePtr(row.DetectedAt)),
			md.Code(string(row.VerificationVerdict)),
			md.Code(string(row.VerificationReason)),
			strconv.Itoa(row.SentCount),
			md.Code(string(row.ReviewStatus)),
			md.Code(renderMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", "))),
		})
	}
	return markdownRows
}

func buildDatasetMarkdownRows(rows []DatasetRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(string(row.AlarmType)),
			md.Code(shared.FallbackSendCountValue(row.ChannelID)),
			md.Code(renderMarkdownCell(row.PostKey)),
			md.Code(shared.FallbackSendCountValue(row.PostID)),
			md.Code(shared.FallbackSendCountValue(row.ContentID)),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTime(row.DetectedAt)),
			md.Code(shared.FormatSendCountTime(row.AlarmSentAt)),
		})
	}
	return markdownRows
}
