package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCompareObservationPostInputs_ClassifiesUnsentDuplicateAndUnexpected(t *testing.T) {
	t.Parallel()

	matchedPublishedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	matchedDetectedAt := matchedPublishedAt.Add(10 * time.Second)
	matchedAlarmSentAt := matchedPublishedAt.Add(50 * time.Second)
	matchedAlarmSentLaterAt := matchedPublishedAt.Add(80 * time.Second)
	unsentPublishedAt := matchedPublishedAt.Add(5 * time.Minute)
	unsentDetectedAt := unsentPublishedAt.Add(12 * time.Second)
	shortPublishedAt := matchedPublishedAt.Add(10 * time.Minute)
	shortDetectedAt := shortPublishedAt.Add(8 * time.Second)
	shortAlarmSentAt := shortPublishedAt.Add(45 * time.Second)
	unexpectedPublishedAt := matchedPublishedAt.Add(20 * time.Minute)
	unexpectedDetectedAt := unexpectedPublishedAt.Add(7 * time.Second)
	unexpectedAlarmSentAt := unexpectedPublishedAt.Add(55 * time.Second)

	baselineInputs := BuildObservationPostComparisonInputsFromBaselines([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            " https://www.youtube.com/post/UgkxDup123?lc=1 ",
			ChannelID:         " UC_COMMUNITY_DUP ",
			ActualPublishedAt: &matchedPublishedAt,
			DetectedAt:        matchedDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            " /post/UgkxUnsent456?lc=1 ",
			ChannelID:         " UC_COMMUNITY_UNSENT ",
			ActualPublishedAt: &unsentPublishedAt,
			DetectedAt:        unsentDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            " AbC123xyZ89 ",
			ChannelID:         " UC_SHORT_MATCHED ",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
	})

	sentInputs := append(
		BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindCommunityPost, []ObservationAlarmSentHistoryRow{
			{
				PostID:            "community:UgkxDup123",
				ContentID:         "UgkxDup123",
				ChannelID:         "UC_COMMUNITY_DUP",
				ActualPublishedAt: &matchedPublishedAt,
				DetectedAt:        matchedDetectedAt,
				AlarmSentAt:       matchedAlarmSentAt,
			},
			{
				PostID:            " UgkxDup123 ",
				ContentID:         " UgkxDup123 ",
				ChannelID:         " UC_COMMUNITY_DUP ",
				ActualPublishedAt: &matchedPublishedAt,
				DetectedAt:        matchedDetectedAt.Add(4 * time.Second),
				AlarmSentAt:       matchedAlarmSentLaterAt,
			},
			{
				PostID:            "community:UgkxUnexpected999",
				ContentID:         "UgkxUnexpected999",
				ChannelID:         "UC_COMMUNITY_UNEXPECTED",
				ActualPublishedAt: &unexpectedPublishedAt,
				DetectedAt:        unexpectedDetectedAt,
				AlarmSentAt:       unexpectedAlarmSentAt,
			},
		}),
		BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindNewShort, []ObservationAlarmSentHistoryRow{
			{
				PostID:            " short:AbC123xyZ89 ",
				ContentID:         " AbC123xyZ89 ",
				ChannelID:         " UC_SHORT_MATCHED ",
				ActualPublishedAt: &shortPublishedAt,
				DetectedAt:        shortDetectedAt,
				AlarmSentAt:       shortAlarmSentAt,
			},
		})...,
	)

	result := CompareObservationPostInputs(baselineInputs, sentInputs)

	require.Equal(t, 3, result.Summary.BaselineInputCount)
	require.Equal(t, 3, result.Summary.BaselineUniquePostCount)
	require.Equal(t, 0, result.Summary.BaselineDuplicateInputCount)
	require.Equal(t, 4, result.Summary.SentInputCount)
	require.Equal(t, 3, result.Summary.SentUniquePostCount)
	require.Equal(t, 1, result.Summary.SentDuplicateInputCount)
	require.Equal(t, 1, result.Summary.MatchedPostCount)
	require.Equal(t, 1, result.Summary.UnsentPostCount)
	require.Equal(t, 1, result.Summary.DuplicateSentPostCount)
	require.Equal(t, 1, result.Summary.UnexpectedSentPostCount)

	require.Len(t, result.MatchedRows, 1)
	require.Equal(t, domain.OutboxKindNewShort, result.MatchedRows[0].Kind)
	require.Equal(t, domain.AlarmTypeShorts, result.MatchedRows[0].AlarmType)
	require.Equal(t, "UC_SHORT_MATCHED", result.MatchedRows[0].ChannelID)
	require.Equal(t, "short:AbC123xyZ89", result.MatchedRows[0].CanonicalPostID)
	require.Equal(t, 1, result.MatchedRows[0].BaselineCount)
	require.Equal(t, 1, result.MatchedRows[0].SentCount)
	require.NotNil(t, result.MatchedRows[0].AlarmSentAt)
	require.Equal(t, shortAlarmSentAt, result.MatchedRows[0].AlarmSentAt.UTC())

	require.Len(t, result.UnsentRows, 1)
	require.Equal(t, domain.OutboxKindCommunityPost, result.UnsentRows[0].Kind)
	require.Equal(t, "UC_COMMUNITY_UNSENT", result.UnsentRows[0].ChannelID)
	require.Equal(t, "community:UgkxUnsent456", result.UnsentRows[0].CanonicalPostID)
	require.Equal(t, 1, result.UnsentRows[0].BaselineCount)
	require.Equal(t, 0, result.UnsentRows[0].SentCount)
	require.Nil(t, result.UnsentRows[0].AlarmSentAt)

	require.Len(t, result.DuplicateSentRows, 1)
	require.Equal(t, domain.OutboxKindCommunityPost, result.DuplicateSentRows[0].Kind)
	require.Equal(t, domain.AlarmTypeCommunity, result.DuplicateSentRows[0].AlarmType)
	require.Equal(t, "UC_COMMUNITY_DUP", result.DuplicateSentRows[0].ChannelID)
	require.Equal(t, "community:UgkxDup123", result.DuplicateSentRows[0].CanonicalPostID)
	require.Equal(t, 1, result.DuplicateSentRows[0].BaselineCount)
	require.Equal(t, 2, result.DuplicateSentRows[0].SentCount)
	require.NotNil(t, result.DuplicateSentRows[0].AlarmSentAt)
	require.Equal(t, matchedAlarmSentAt, result.DuplicateSentRows[0].AlarmSentAt.UTC())

	require.Len(t, result.UnexpectedSentRows, 1)
	require.Equal(t, domain.OutboxKindCommunityPost, result.UnexpectedSentRows[0].Kind)
	require.Equal(t, "UC_COMMUNITY_UNEXPECTED", result.UnexpectedSentRows[0].ChannelID)
	require.Equal(t, "community:UgkxUnexpected999", result.UnexpectedSentRows[0].CanonicalPostID)
	require.Equal(t, 0, result.UnexpectedSentRows[0].BaselineCount)
	require.Equal(t, 1, result.UnexpectedSentRows[0].SentCount)
	require.NotNil(t, result.UnexpectedSentRows[0].AlarmSentAt)
	require.Equal(t, unexpectedAlarmSentAt, result.UnexpectedSentRows[0].AlarmSentAt.UTC())

	require.Len(t, result.VerdictRows, 4)
	require.Equal(t, ObservationPostComparisonVerdictMatched, result.VerdictRows[0].Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched, result.VerdictRows[0].Reason)
	require.Equal(t, "short:AbC123xyZ89", result.VerdictRows[0].CanonicalPostID)
	require.Equal(t, 1, result.VerdictRows[0].BaselineCount)
	require.Equal(t, 1, result.VerdictRows[0].SentCount)
	require.Equal(t, ObservationPostComparisonVerdictUnsent, result.VerdictRows[1].Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory, result.VerdictRows[1].Reason)
	require.Equal(t, "community:UgkxUnsent456", result.VerdictRows[1].CanonicalPostID)
	require.Equal(t, ObservationPostComparisonVerdictDuplicateSent, result.VerdictRows[2].Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonMultipleSentRowsForCanonicalPost, result.VerdictRows[2].Reason)
	require.Equal(t, 2, result.VerdictRows[2].SentCount)
	require.Equal(t, ObservationPostComparisonVerdictUnexpectedSent, result.VerdictRows[3].Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonSentHistoryWithoutBaseline, result.VerdictRows[3].Reason)
	require.Equal(t, "community:UgkxUnexpected999", result.VerdictRows[3].CanonicalPostID)
}

func TestCompareObservationPostInputs_DeduplicatesBaselineInputsBeforeClassifying(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(9 * time.Second)
	detectedEarlierAt := publishedAt.Add(7 * time.Second)
	alarmSentAt := publishedAt.Add(44 * time.Second)

	baselineInputs := BuildObservationPostComparisonInputsFromBaselines([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            " /post/UgkxShared123?lc=1 ",
			ChannelID:         " UC_SHARED ",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            " community:UgkxShared123 ",
			ChannelID:         "UC_SHARED",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedEarlierAt,
		},
	})
	sentInputs := BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindCommunityPost, []ObservationAlarmSentHistoryRow{
		{
			PostID:            " ",
			ContentID:         "UgkxShared123",
			ChannelID:         " UC_SHARED ",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
			AlarmSentAt:       alarmSentAt,
		},
	})

	result := CompareObservationPostInputs(baselineInputs, sentInputs)

	require.Equal(t, 2, result.Summary.BaselineInputCount)
	require.Equal(t, 1, result.Summary.BaselineUniquePostCount)
	require.Equal(t, 1, result.Summary.BaselineDuplicateInputCount)
	require.Equal(t, 1, result.Summary.SentInputCount)
	require.Equal(t, 1, result.Summary.SentUniquePostCount)
	require.Equal(t, 0, result.Summary.SentDuplicateInputCount)
	require.Equal(t, 1, result.Summary.MatchedPostCount)
	require.Equal(t, 0, result.Summary.UnsentPostCount)
	require.Equal(t, 0, result.Summary.DuplicateSentPostCount)
	require.Equal(t, 0, result.Summary.UnexpectedSentPostCount)
	require.Len(t, result.MatchedRows, 1)
	require.Equal(t, "community:UgkxShared123", result.MatchedRows[0].CanonicalPostID)
	require.Equal(t, 2, result.MatchedRows[0].BaselineCount)
	require.Equal(t, 1, result.MatchedRows[0].SentCount)
	require.NotNil(t, result.MatchedRows[0].DetectedAt)
	require.Equal(t, detectedEarlierAt, result.MatchedRows[0].DetectedAt.UTC())
	require.NotNil(t, result.MatchedRows[0].AlarmSentAt)
	require.Equal(t, alarmSentAt, result.MatchedRows[0].AlarmSentAt.UTC())
}

func TestCompareObservationPostInputs_ClassifiesIdentifierMismatchCandidatesByPublishedAtAndTitleHint(t *testing.T) {
	t.Parallel()

	baselinePublishedAt := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
	sentPublishedAt := baselinePublishedAt.Add(20 * time.Second)
	baselineDetectedAt := baselinePublishedAt.Add(10 * time.Second)
	sentDetectedAt := sentPublishedAt.Add(10 * time.Second)
	sentAlarmSentAt := sentPublishedAt.Add(45 * time.Second)

	result := CompareObservationPostInputs(
		[]ObservationPostComparisonInput{{
			Kind:              domain.OutboxKindCommunityPost,
			AlarmType:         domain.AlarmTypeCommunity,
			CanonicalPostID:   "community:baseline-post",
			ChannelID:         "UC_REVIEW",
			TitleHint:         "Same   title hint",
			ActualPublishedAt: &baselinePublishedAt,
			DetectedAt:        &baselineDetectedAt,
		}},
		[]ObservationPostComparisonInput{{
			Kind:              domain.OutboxKindCommunityPost,
			AlarmType:         domain.AlarmTypeCommunity,
			CanonicalPostID:   "community:sent-post",
			ContentID:         "sent-post",
			ChannelID:         "UC_REVIEW",
			TitleHint:         "same title hint",
			ActualPublishedAt: &sentPublishedAt,
			DetectedAt:        &sentDetectedAt,
			AlarmSentAt:       &sentAlarmSentAt,
		}},
	)

	require.Equal(t, 1, result.Summary.IdentifierMismatchCandidateCount)
	require.Equal(t, 0, result.Summary.UnsentPostCount)
	require.Equal(t, 0, result.Summary.UnexpectedSentPostCount)
	require.Len(t, result.IdentifierMismatchCandidates, 1)

	candidate := result.IdentifierMismatchCandidates[0]
	require.Equal(t, ObservationIdentifierMismatchCandidateReviewStatusPendingReview, candidate.ReviewStatus)
	require.Equal(t, domain.OutboxKindCommunityPost, candidate.Kind)
	require.Equal(t, domain.AlarmTypeCommunity, candidate.AlarmType)
	require.Equal(t, "UC_REVIEW", candidate.ChannelID)
	require.NotNil(t, candidate.MatchPublishedAt)
	require.Equal(t, baselinePublishedAt, candidate.MatchPublishedAt.UTC())
	require.Equal(t, "Same title hint", candidate.MatchTitleHint)
	require.Equal(t, []string{"actual_published_at", "title_hint"}, candidate.MatchBasis)
	require.Len(t, candidate.BaselineRows, 1)
	require.Equal(t, "community:baseline-post", candidate.BaselineRows[0].CanonicalPostID)
	require.Equal(t, "Same title hint", candidate.BaselineRows[0].TitleHint)
	require.Len(t, candidate.SentRows, 1)
	require.Equal(t, "community:sent-post", candidate.SentRows[0].CanonicalPostID)
	require.Equal(t, "same title hint", candidate.SentRows[0].TitleHint)

	require.Len(t, result.VerdictRows, 1)
	verdict := result.VerdictRows[0]
	require.Equal(t, ObservationPostComparisonVerdictIdentifierMismatchCandidate, verdict.Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview, verdict.Reason)
	require.Equal(t, domain.OutboxKindCommunityPost, verdict.Kind)
	require.Equal(t, domain.AlarmTypeCommunity, verdict.AlarmType)
	require.Equal(t, "UC_REVIEW", verdict.ChannelID)
	require.NotNil(t, verdict.MatchPublishedAt)
	require.Equal(t, baselinePublishedAt, verdict.MatchPublishedAt.UTC())
	require.Equal(t, "Same title hint", verdict.MatchTitleHint)
	require.Equal(t, []string{"actual_published_at", "title_hint"}, verdict.MatchBasis)
	require.Equal(t, ObservationIdentifierMismatchCandidateReviewStatusPendingReview, verdict.ReviewStatus)
	require.Equal(t, []string{"community:baseline-post"}, verdict.RelatedBaselinePostIDs)
	require.Equal(t, []string{"community:sent-post"}, verdict.RelatedSentPostIDs)
}
