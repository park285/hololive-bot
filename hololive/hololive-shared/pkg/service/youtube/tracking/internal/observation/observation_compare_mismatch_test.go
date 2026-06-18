package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCompareObservationPostInputsMismatchEdgeCases(t *testing.T) {
	t.Run("empty inputs", func(t *testing.T) {
		result := CompareObservationPostInputs(nil, nil)

		require.Equal(t, ObservationPostComparisonSummary{}, result.Summary)
		require.Empty(t, result.MatchedRows)
		require.Empty(t, result.UnsentRows)
		require.Empty(t, result.UnexpectedSentRows)
		require.Empty(t, result.IdentifierMismatchCandidates)
		require.Empty(t, result.VerdictRows)
	})

	t.Run("identical inputs do not create mismatch candidate", func(t *testing.T) {
		publishedAt := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
		detectedAt := publishedAt.Add(10 * time.Second)
		alarmSentAt := publishedAt.Add(time.Minute)
		input := observationMismatchComparisonInput(
			domain.OutboxKindCommunityPost,
			"community:same-post",
			"",
			"UC_SAME",
			"Same title",
			publishedAt,
			detectedAt,
			nil,
		)
		sent := input
		sent.AlarmSentAt = testObservationMismatchTimePtr(alarmSentAt)

		result := CompareObservationPostInputs([]ObservationPostComparisonInput{input}, []ObservationPostComparisonInput{sent})

		require.Equal(t, 1, result.Summary.MatchedPostCount)
		require.Equal(t, 0, result.Summary.IdentifierMismatchCandidateCount)
		require.Len(t, result.MatchedRows, 1)
		require.Equal(t, "community:same-post", result.MatchedRows[0].CanonicalPostID)
		require.Empty(t, result.IdentifierMismatchCandidates)
		require.Empty(t, result.UnsentRows)
		require.Empty(t, result.UnexpectedSentRows)
	})

	t.Run("missing auxiliary fields do not create mismatch candidate", func(t *testing.T) {
		publishedAt := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
		baseline := observationMismatchComparisonInput(
			domain.OutboxKindCommunityPost,
			"community:missing-channel-baseline",
			"",
			"",
			"Same title",
			publishedAt,
			publishedAt.Add(10*time.Second),
			nil,
		)
		sent := observationMismatchComparisonInput(
			domain.OutboxKindCommunityPost,
			"community:missing-channel-sent",
			"",
			"",
			"same title",
			publishedAt,
			publishedAt.Add(20*time.Second),
			testObservationMismatchTimePtr(publishedAt.Add(time.Minute)),
		)

		result := CompareObservationPostInputs([]ObservationPostComparisonInput{baseline}, []ObservationPostComparisonInput{sent})

		require.Equal(t, 0, result.Summary.IdentifierMismatchCandidateCount)
		require.Len(t, result.UnsentRows, 1)
		require.Len(t, result.UnexpectedSentRows, 1)
		require.Empty(t, result.IdentifierMismatchCandidates)
	})
}

func TestCompareObservationPostInputsDetectsIdentifierMismatchPatterns(t *testing.T) {
	basePublishedAt := time.Date(2026, 4, 10, 5, 0, 10, 0, time.UTC)

	tests := []struct {
		name                    string
		baseline                ObservationPostComparisonInput
		sent                    ObservationPostComparisonInput
		wantMismatch            bool
		wantMatchPublishedAt    time.Time
		wantMatchTitleHint      string
		wantMatchBasis          []string
		wantUnsentCount         int
		wantUnexpectedSentCount int
	}{
		{
			name: "post id differs with same published_at",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:baseline-post",
				"",
				"UC_MATCH",
				"",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:sent-post",
				"sent-post",
				"UC_MATCH",
				"",
				basePublishedAt,
				basePublishedAt.Add(20*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantMismatch:         true,
			wantMatchPublishedAt: basePublishedAt,
			wantMatchBasis:       []string{"actual_published_at"},
		},
		{
			name: "post id and seconds differ with same title hint in same minute",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindNewShort,
				"short:baseline-short",
				"",
				"UC_MATCH",
				"Launch   clip",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindNewShort,
				"short:sent-short",
				"sent-short",
				"UC_MATCH",
				"launch clip",
				basePublishedAt.Add(35*time.Second),
				basePublishedAt.Add(45*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantMismatch:         true,
			wantMatchPublishedAt: basePublishedAt,
			wantMatchTitleHint:   "Launch clip",
			wantMatchBasis:       []string{"actual_published_at", "title_hint"},
		},
		{
			name: "channel differs",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:baseline-channel",
				"",
				"UC_BASELINE",
				"Same title",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:sent-channel",
				"sent-channel",
				"UC_SENT",
				"same title",
				basePublishedAt,
				basePublishedAt.Add(20*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantUnsentCount:         1,
			wantUnexpectedSentCount: 1,
		},
		{
			name: "published_at differs without title hint",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:baseline-published-at",
				"",
				"UC_MATCH",
				"",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:sent-published-at",
				"sent-published-at",
				"UC_MATCH",
				"",
				basePublishedAt.Add(time.Second),
				basePublishedAt.Add(20*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantUnsentCount:         1,
			wantUnexpectedSentCount: 1,
		},
		{
			name: "title hint differs",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:baseline-title",
				"",
				"UC_MATCH",
				"First title",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:sent-title",
				"sent-title",
				"UC_MATCH",
				"Second title",
				basePublishedAt,
				basePublishedAt.Add(20*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantUnsentCount:         1,
			wantUnexpectedSentCount: 1,
		},
		{
			name: "kind differs",
			baseline: observationMismatchComparisonInput(
				domain.OutboxKindCommunityPost,
				"community:baseline-kind",
				"",
				"UC_MATCH",
				"Same title",
				basePublishedAt,
				basePublishedAt.Add(10*time.Second),
				nil,
			),
			sent: observationMismatchComparisonInput(
				domain.OutboxKindNewShort,
				"short:sent-kind",
				"sent-kind",
				"UC_MATCH",
				"same title",
				basePublishedAt,
				basePublishedAt.Add(20*time.Second),
				testObservationMismatchTimePtr(basePublishedAt.Add(time.Minute)),
			),
			wantUnsentCount:         1,
			wantUnexpectedSentCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareObservationPostInputs(
				[]ObservationPostComparisonInput{tt.baseline},
				[]ObservationPostComparisonInput{tt.sent},
			)

			if tt.wantMismatch {
				require.Equal(t, 1, result.Summary.IdentifierMismatchCandidateCount)
				require.Empty(t, result.UnsentRows)
				require.Empty(t, result.UnexpectedSentRows)
				require.Len(t, result.IdentifierMismatchCandidates, 1)

				candidate := result.IdentifierMismatchCandidates[0]
				require.Equal(t, tt.baseline.Kind, candidate.Kind)
				require.Equal(t, tt.baseline.Kind.ToAlarmType(), candidate.AlarmType)
				require.Equal(t, tt.baseline.ChannelID, candidate.ChannelID)
				require.NotNil(t, candidate.MatchPublishedAt)
				require.Equal(t, tt.wantMatchPublishedAt, candidate.MatchPublishedAt.UTC())
				require.Equal(t, tt.wantMatchTitleHint, candidate.MatchTitleHint)
				require.Equal(t, tt.wantMatchBasis, candidate.MatchBasis)
				require.Equal(t, ObservationIdentifierMismatchCandidateReviewStatusPendingReview, candidate.ReviewStatus)
				require.Len(t, candidate.BaselineRows, 1)
				require.Equal(t, tt.baseline.CanonicalPostID, candidate.BaselineRows[0].CanonicalPostID)
				require.Len(t, candidate.SentRows, 1)
				require.Equal(t, tt.sent.CanonicalPostID, candidate.SentRows[0].CanonicalPostID)
				require.Len(t, result.VerdictRows, 1)
				require.Equal(t, ObservationPostComparisonVerdictIdentifierMismatchCandidate, result.VerdictRows[0].Verdict)
				require.Equal(t, ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview, result.VerdictRows[0].Reason)
				return
			}

			require.Equal(t, 0, result.Summary.IdentifierMismatchCandidateCount)
			require.Len(t, result.UnsentRows, tt.wantUnsentCount)
			require.Len(t, result.UnexpectedSentRows, tt.wantUnexpectedSentCount)
			require.Empty(t, result.IdentifierMismatchCandidates)
		})
	}
}

func TestObservationIdentifierMismatchCandidateSorting(t *testing.T) {
	earlyPublishedAt := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
	laterPublishedAt := earlyPublishedAt.Add(time.Minute)

	candidates := []ObservationIdentifierMismatchCandidate{
		{
			Kind:             domain.OutboxKindNewShort,
			ChannelID:        "UC_A",
			MatchPublishedAt: testObservationMismatchTimePtr(earlyPublishedAt),
			MatchTitleHint:   "A",
			BaselineRows:     []ObservationPostComparisonRow{{CanonicalPostID: "short:new-short"}},
		},
		{
			Kind:             domain.OutboxKindCommunityPost,
			ChannelID:        "UC_B",
			MatchPublishedAt: testObservationMismatchTimePtr(earlyPublishedAt),
			MatchTitleHint:   "A",
			BaselineRows:     []ObservationPostComparisonRow{{CanonicalPostID: "community:channel-b"}},
		},
		{
			Kind:             domain.OutboxKindCommunityPost,
			ChannelID:        "UC_A",
			MatchPublishedAt: testObservationMismatchTimePtr(laterPublishedAt),
			MatchTitleHint:   "A",
			BaselineRows:     []ObservationPostComparisonRow{{CanonicalPostID: "community:later"}},
		},
		{
			Kind:             domain.OutboxKindCommunityPost,
			ChannelID:        "UC_A",
			MatchPublishedAt: testObservationMismatchTimePtr(earlyPublishedAt),
			MatchTitleHint:   "Z",
			BaselineRows:     []ObservationPostComparisonRow{{CanonicalPostID: "community:title-z"}},
		},
		{
			Kind:             domain.OutboxKindCommunityPost,
			ChannelID:        "UC_A",
			MatchPublishedAt: testObservationMismatchTimePtr(earlyPublishedAt),
			MatchTitleHint:   "A",
			BaselineRows: []ObservationPostComparisonRow{
				{CanonicalPostID: "community:title-a-1"},
				{CanonicalPostID: "community:title-a-2"},
			},
		},
		{
			Kind:             domain.OutboxKindCommunityPost,
			ChannelID:        "UC_A",
			MatchPublishedAt: testObservationMismatchTimePtr(earlyPublishedAt),
			MatchTitleHint:   "A",
			BaselineRows:     []ObservationPostComparisonRow{{CanonicalPostID: "community:title-a"}},
		},
	}

	sortObservationIdentifierMismatchCandidates(candidates)

	require.Equal(t, []string{
		"community:title-a",
		"community:title-a-1",
		"community:title-z",
		"community:later",
		"community:channel-b",
		"short:new-short",
	}, []string{
		candidates[0].BaselineRows[0].CanonicalPostID,
		candidates[1].BaselineRows[0].CanonicalPostID,
		candidates[2].BaselineRows[0].CanonicalPostID,
		candidates[3].BaselineRows[0].CanonicalPostID,
		candidates[4].BaselineRows[0].CanonicalPostID,
		candidates[5].BaselineRows[0].CanonicalPostID,
	})
}

func TestObservationPostComparisonRowSortingAndComparators(t *testing.T) {
	earlyDetectedAt := time.Date(2026, 4, 10, 5, 0, 0, 0, time.UTC)
	laterDetectedAt := earlyDetectedAt.Add(time.Minute)
	earlyAlarmSentAt := earlyDetectedAt.Add(2 * time.Minute)
	laterAlarmSentAt := earlyDetectedAt.Add(3 * time.Minute)

	rows := []ObservationPostComparisonRow{
		{
			Kind:            domain.OutboxKindNewShort,
			ChannelID:       "UC_A",
			CanonicalPostID: "short:row",
			ContentID:       "row",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "A",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_B",
			CanonicalPostID: "community:row",
			ContentID:       "row",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "A",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_A",
			CanonicalPostID: "community:z",
			ContentID:       "z",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "A",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_A",
			CanonicalPostID: "community:a",
			ContentID:       "z",
			DetectedAt:      testObservationMismatchTimePtr(laterDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "A",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_A",
			CanonicalPostID: "community:a",
			ContentID:       "a",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(laterAlarmSentAt),
			TitleHint:       "Z",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_A",
			CanonicalPostID: "community:a",
			ContentID:       "a",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "B",
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			ChannelID:       "UC_A",
			CanonicalPostID: "community:a",
			ContentID:       "b",
			DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
			AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
			TitleHint:       "A",
		},
	}

	sortObservationPostComparisonRows(rows)

	require.Equal(t, []string{
		"community:a/a/B",
		"community:a/b/A",
		"community:a/a/Z",
		"community:a/z/A",
		"community:z/z/A",
		"community:row/row/A",
		"short:row/row/A",
	}, observationPostComparisonRowSortKeys(rows))

	base := ObservationPostComparisonRow{
		Kind:            domain.OutboxKindCommunityPost,
		ChannelID:       "UC_A",
		CanonicalPostID: "community:a",
		ContentID:       "a",
		DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
		AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
		TitleHint:       "A",
	}
	require.Zero(t, compareObservationPostComparisonRows(&base, &base))
	require.Negative(t, compareObservationPostComparisonRowKind(&base, &ObservationPostComparisonRow{Kind: domain.OutboxKindNewShort}))
	require.Negative(t, compareObservationPostComparisonRowChannelID(&base, &ObservationPostComparisonRow{ChannelID: "UC_B"}))
	require.Negative(t, compareObservationPostComparisonRowCanonicalPostID(&base, &ObservationPostComparisonRow{CanonicalPostID: "community:b"}))
	require.Negative(t, compareObservationPostComparisonRowDetectedAt(&base, &ObservationPostComparisonRow{DetectedAt: testObservationMismatchTimePtr(laterDetectedAt)}))
	require.Negative(t, compareObservationPostComparisonRowAlarmSentAt(&base, &ObservationPostComparisonRow{AlarmSentAt: testObservationMismatchTimePtr(laterAlarmSentAt)}))
	require.Negative(t, compareObservationPostComparisonRowContentID(&base, &ObservationPostComparisonRow{ContentID: "b"}))
	require.Negative(t, compareObservationPostComparisonRowTitleHint(&base, &ObservationPostComparisonRow{TitleHint: "B"}))
	require.Negative(t, compareObservationPostComparisonRows(&base, &ObservationPostComparisonRow{Kind: domain.OutboxKindNewShort}))
	require.Negative(t, compareObservationPostComparisonRows(&base, &ObservationPostComparisonRow{
		Kind:            domain.OutboxKindCommunityPost,
		ChannelID:       "UC_A",
		CanonicalPostID: "community:a",
		ContentID:       "a",
		DetectedAt:      testObservationMismatchTimePtr(earlyDetectedAt),
		AlarmSentAt:     testObservationMismatchTimePtr(earlyAlarmSentAt),
		TitleHint:       "B",
	}))
}

func observationMismatchComparisonInput(
	kind domain.OutboxKind,
	canonicalPostID string,
	contentID string,
	channelID string,
	titleHint string,
	actualPublishedAt time.Time,
	detectedAt time.Time,
	alarmSentAt *time.Time,
) ObservationPostComparisonInput {
	return ObservationPostComparisonInput{
		Kind:              kind,
		AlarmType:         kind.ToAlarmType(),
		CanonicalPostID:   canonicalPostID,
		ContentID:         contentID,
		ChannelID:         channelID,
		TitleHint:         titleHint,
		ActualPublishedAt: testObservationMismatchTimePtr(actualPublishedAt),
		DetectedAt:        testObservationMismatchTimePtr(detectedAt),
		AlarmSentAt:       alarmSentAt,
	}
}

func testObservationMismatchTimePtr(value time.Time) *time.Time {
	normalized := value.UTC()
	return &normalized
}

func observationPostComparisonRowSortKeys(rows []ObservationPostComparisonRow) []string {
	keys := make([]string, 0, len(rows))
	for i := range rows {
		keys = append(keys, rows[i].CanonicalPostID+"/"+rows[i].ContentID+"/"+rows[i].TitleHint)
	}
	return keys
}
