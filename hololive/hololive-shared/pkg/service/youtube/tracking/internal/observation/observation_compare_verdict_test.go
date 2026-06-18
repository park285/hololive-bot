package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCollectObservationPostComparisonCanonicalPostIDs_ExtractsNonEmpty(t *testing.T) {
	t.Parallel()

	rows := []ObservationPostComparisonRow{
		{CanonicalPostID: " community:post-1 ", ContentID: "fallback-1"},
		{CanonicalPostID: "", ContentID: " content-only "},
		{CanonicalPostID: "", ContentID: ""},
		{CanonicalPostID: "community:post-3"},
	}
	result := collectObservationPostComparisonCanonicalPostIDs(rows)
	require.Equal(t, []string{"community:post-1", "content-only", "community:post-3"}, result)
}

func TestCollectObservationPostComparisonCanonicalPostIDs_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, collectObservationPostComparisonCanonicalPostIDs(nil))
	require.Empty(t, collectObservationPostComparisonCanonicalPostIDs([]ObservationPostComparisonRow{}))
}

func TestCloneObservationPostComparisonMatchBasis_ClonesAndTrims(t *testing.T) {
	t.Parallel()

	original := []string{" actual_published_at ", "title_hint"}
	result := cloneObservationPostComparisonMatchBasis(original)
	require.NotNil(t, result)
	require.Equal(t, []string{"actual_published_at", "title_hint"}, result)

	original[0] = "mutated"
	require.Equal(t, "actual_published_at", result[0])
}

func TestCloneObservationPostComparisonMatchBasis_NilAndEmptyReturnNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, cloneObservationPostComparisonMatchBasis(nil))
	require.Nil(t, cloneObservationPostComparisonMatchBasis([]string{}))
}

func TestCloneObservationPostComparisonMatchBasis_AllBlankStringsReturnNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, cloneObservationPostComparisonMatchBasis([]string{"  ", "", " "}))
}

func TestBuildObservationPostComparisonVerdictRowFromRow_MapsFieldsCorrectly(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(10 * time.Second)
	alarmSentAt := publishedAt.Add(50 * time.Second)

	row := ObservationPostComparisonRow{
		Kind:              domain.OutboxKindCommunityPost,
		AlarmType:         domain.AlarmTypeCommunity,
		ChannelID:         "  UC_CHANNEL  ",
		CanonicalPostID:   " community:post-1 ",
		ContentID:         " content-1 ",
		TitleHint:         "  Hello   World  ",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        &detectedAt,
		AlarmSentAt:       &alarmSentAt,
		BaselineCount:     2,
		SentCount:         1,
	}

	verdict := buildObservationPostComparisonVerdictRowFromRow(&row, ObservationPostComparisonVerdictMatched,
		ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
	)

	require.Equal(t, ObservationPostComparisonVerdictMatched, verdict.Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched, verdict.Reason)
	require.Equal(t, domain.OutboxKindCommunityPost, verdict.Kind)
	require.Equal(t, domain.AlarmTypeCommunity, verdict.AlarmType)
	require.Equal(t, "UC_CHANNEL", verdict.ChannelID)
	require.Equal(t, "community:post-1", verdict.CanonicalPostID)
	require.Equal(t, "content-1", verdict.ContentID)
	require.Equal(t, "Hello World", verdict.TitleHint)
	require.NotNil(t, verdict.ActualPublishedAt)
	require.Equal(t, publishedAt, verdict.ActualPublishedAt.UTC())
	require.NotNil(t, verdict.DetectedAt)
	require.Equal(t, detectedAt, verdict.DetectedAt.UTC())
	require.NotNil(t, verdict.AlarmSentAt)
	require.Equal(t, alarmSentAt, verdict.AlarmSentAt.UTC())
	require.Equal(t, 2, verdict.BaselineCount)
	require.Equal(t, 1, verdict.SentCount)
}

func TestBuildObservationPostComparisonVerdictRowFromCandidate_MapsFieldsCorrectly(t *testing.T) {
	t.Parallel()

	matchPublishedAt := time.Date(2026, 5, 10, 2, 0, 0, 0, time.UTC)

	candidate := ObservationIdentifierMismatchCandidate{
		Kind:             domain.OutboxKindNewShort,
		AlarmType:        domain.AlarmTypeShorts,
		ChannelID:        "  UC_SHORT  ",
		MatchPublishedAt: &matchPublishedAt,
		MatchTitleHint:   "  Test   Title  ",
		MatchBasis:       []string{"actual_published_at", "title_hint"},
		ReviewStatus:     ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
		BaselineRows: []ObservationPostComparisonRow{
			{CanonicalPostID: "short:baseline-1"},
			{CanonicalPostID: "", ContentID: "fallback-content"},
		},
		SentRows: []ObservationPostComparisonRow{
			{CanonicalPostID: "short:sent-1"},
		},
	}

	verdict := buildObservationPostComparisonVerdictRowFromCandidate(&candidate)

	require.Equal(t, ObservationPostComparisonVerdictIdentifierMismatchCandidate, verdict.Verdict)
	require.Equal(t, ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview, verdict.Reason)
	require.Equal(t, domain.OutboxKindNewShort, verdict.Kind)
	require.Equal(t, domain.AlarmTypeShorts, verdict.AlarmType)
	require.Equal(t, "UC_SHORT", verdict.ChannelID)
	require.NotNil(t, verdict.MatchPublishedAt)
	require.Equal(t, matchPublishedAt, verdict.MatchPublishedAt.UTC())
	require.Equal(t, "Test Title", verdict.MatchTitleHint)
	require.Equal(t, []string{"actual_published_at", "title_hint"}, verdict.MatchBasis)
	require.Equal(t, ObservationIdentifierMismatchCandidateReviewStatusPendingReview, verdict.ReviewStatus)
	require.Equal(t, 2, verdict.BaselineCount)
	require.Equal(t, 1, verdict.SentCount)
	require.Equal(t, []string{"short:baseline-1", "fallback-content"}, verdict.RelatedBaselinePostIDs)
	require.Equal(t, []string{"short:sent-1"}, verdict.RelatedSentPostIDs)
}

func TestBuildObservationPostComparisonVerdictRows_ConcatenatesAllCategories(t *testing.T) {
	t.Parallel()

	matchedRow := ObservationPostComparisonRow{
		Kind:            domain.OutboxKindCommunityPost,
		AlarmType:       domain.AlarmTypeCommunity,
		ChannelID:       "UC1",
		CanonicalPostID: "community:matched",
		BaselineCount:   1,
		SentCount:       1,
	}
	unsentRow := ObservationPostComparisonRow{
		Kind:            domain.OutboxKindCommunityPost,
		AlarmType:       domain.AlarmTypeCommunity,
		ChannelID:       "UC2",
		CanonicalPostID: "community:unsent",
		BaselineCount:   1,
	}

	result := ObservationPostComparisonResult{
		MatchedRows: []ObservationPostComparisonRow{matchedRow},
		UnsentRows:  []ObservationPostComparisonRow{unsentRow},
	}

	verdicts := buildObservationPostComparisonVerdictRows(&result)
	require.Len(t, verdicts, 2)
	require.Equal(t, ObservationPostComparisonVerdictMatched, verdicts[0].Verdict)
	require.Equal(t, "community:matched", verdicts[0].CanonicalPostID)
	require.Equal(t, ObservationPostComparisonVerdictUnsent, verdicts[1].Verdict)
	require.Equal(t, "community:unsent", verdicts[1].CanonicalPostID)
}
