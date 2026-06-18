package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestEarliestObservationPostComparisonTime_BothNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, earliestObservationPostComparisonTime(nil, nil))
}

func TestEarliestObservationPostComparisonTime_LeftNilReturnsRight(t *testing.T) {
	t.Parallel()

	right := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	result := earliestObservationPostComparisonTime(nil, &right)
	require.NotNil(t, result)
	require.Equal(t, right, result.UTC())
}

func TestEarliestObservationPostComparisonTime_RightNilReturnsLeft(t *testing.T) {
	t.Parallel()

	left := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	result := earliestObservationPostComparisonTime(&left, nil)
	require.NotNil(t, result)
	require.Equal(t, left, result.UTC())
}

func TestEarliestObservationPostComparisonTime_ReturnsEarlier(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)

	result := earliestObservationPostComparisonTime(&later, &earlier)
	require.NotNil(t, result)
	require.Equal(t, earlier, result.UTC())

	result = earliestObservationPostComparisonTime(&earlier, &later)
	require.NotNil(t, result)
	require.Equal(t, earlier, result.UTC())
}

func TestEarliestObservationPostComparisonTime_ClonesPointer(t *testing.T) {
	t.Parallel()

	original := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	result := earliestObservationPostComparisonTime(&original, nil)
	require.NotNil(t, result)
	require.NotSame(t, &original, result)
}

func TestMergeObservationPostComparisonInputs_PreservesEarliestTimes(t *testing.T) {
	t.Parallel()

	early := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	late := early.Add(time.Minute)

	left := ObservationPostComparisonInput{
		Kind:            domain.OutboxKindCommunityPost,
		AlarmType:       domain.AlarmTypeCommunity,
		CanonicalPostID: "community:post-1",
		ChannelID:       "UC_TEST",
		DetectedAt:      &late,
		AlarmSentAt:     &late,
	}
	right := ObservationPostComparisonInput{
		Kind:        domain.OutboxKindCommunityPost,
		DetectedAt:  &early,
		AlarmSentAt: &early,
	}

	merged := mergeObservationPostComparisonInputs(&left, &right)
	require.Equal(t, domain.OutboxKindCommunityPost, merged.Kind)
	require.Equal(t, "community:post-1", merged.CanonicalPostID)
	require.Equal(t, "UC_TEST", merged.ChannelID)
	require.NotNil(t, merged.DetectedAt)
	require.Equal(t, early, merged.DetectedAt.UTC())
	require.NotNil(t, merged.AlarmSentAt)
	require.Equal(t, early, merged.AlarmSentAt.UTC())
}

func TestMergeObservationPostComparisonInputs_FillsBlanks(t *testing.T) {
	t.Parallel()

	detected := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)

	left := ObservationPostComparisonInput{
		Kind:       domain.OutboxKindNewShort,
		ChannelID:  "",
		DetectedAt: &detected,
	}
	right := ObservationPostComparisonInput{
		ChannelID:  "UC_FILLED",
		ContentID:  "content-1",
		DetectedAt: &detected,
	}

	merged := mergeObservationPostComparisonInputs(&left, &right)
	require.Equal(t, "UC_FILLED", merged.ChannelID)
	require.Equal(t, "content-1", merged.ContentID)
	require.Equal(t, domain.OutboxKindNewShort, merged.Kind)
}

func TestBuildObservationPostComparisonKey_MissingPostIDGeneratesFallback(t *testing.T) {
	t.Parallel()

	detected := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	input := ObservationPostComparisonInput{
		Kind:       domain.OutboxKindCommunityPost,
		ChannelID:  "UC_TEST",
		DetectedAt: &detected,
	}

	key := buildObservationPostComparisonKey(&input, 0)
	require.Equal(t, domain.OutboxKindCommunityPost, key.kind)
	require.Equal(t, "UC_TEST", key.channelID)
	require.Contains(t, key.canonicalPostID, "__missing_post_id__:")
	require.Contains(t, key.canonicalPostID, "UC_TEST")
}

func TestBuildObservationPostComparisonKey_UsesCanonicalPostID(t *testing.T) {
	t.Parallel()

	input := ObservationPostComparisonInput{
		Kind:            domain.OutboxKindCommunityPost,
		CanonicalPostID: "community:post-1",
		ChannelID:       "UC_TEST",
	}

	key := buildObservationPostComparisonKey(&input, 0)
	require.Equal(t, "community:post-1", key.canonicalPostID)
	require.Equal(t, "UC_TEST", key.channelID)
}

func TestBuildObservationPostComparisonRow_MergesBaselineAndSent(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	detected := published.Add(10 * time.Second)
	sentAt := published.Add(50 * time.Second)

	baseline := &observationPostComparisonAccumulator{
		representative: ObservationPostComparisonInput{
			Kind:              domain.OutboxKindCommunityPost,
			AlarmType:         domain.AlarmTypeCommunity,
			CanonicalPostID:   "community:post-1",
			ChannelID:         "UC_TEST",
			ActualPublishedAt: &published,
			DetectedAt:        &detected,
		},
		count: 2,
	}
	sent := &observationPostComparisonAccumulator{
		representative: ObservationPostComparisonInput{
			Kind:            domain.OutboxKindCommunityPost,
			CanonicalPostID: "community:post-1",
			ChannelID:       "UC_TEST",
			AlarmSentAt:     &sentAt,
		},
		count: 1,
	}

	row := buildObservationPostComparisonRow(baseline, sent)
	require.Equal(t, domain.OutboxKindCommunityPost, row.Kind)
	require.Equal(t, "community:post-1", row.CanonicalPostID)
	require.Equal(t, "UC_TEST", row.ChannelID)
	require.NotNil(t, row.ActualPublishedAt)
	require.Equal(t, published, row.ActualPublishedAt.UTC())
	require.NotNil(t, row.DetectedAt)
	require.Equal(t, detected, row.DetectedAt.UTC())
	require.NotNil(t, row.AlarmSentAt)
	require.Equal(t, sentAt, row.AlarmSentAt.UTC())
	require.Equal(t, 2, row.BaselineCount)
	require.Equal(t, 1, row.SentCount)
}

func TestBuildObservationPostComparisonRow_NilBaseline(t *testing.T) {
	t.Parallel()

	sentAt := time.Date(2026, 5, 10, 1, 0, 50, 0, time.UTC)
	sent := &observationPostComparisonAccumulator{
		representative: ObservationPostComparisonInput{
			Kind:            domain.OutboxKindNewShort,
			CanonicalPostID: "short:video-1",
			ChannelID:       "UC_SHORT",
			AlarmSentAt:     &sentAt,
		},
		count: 1,
	}

	row := buildObservationPostComparisonRow(nil, sent)
	require.Equal(t, domain.OutboxKindNewShort, row.Kind)
	require.Equal(t, "short:video-1", row.CanonicalPostID)
	require.Equal(t, 0, row.BaselineCount)
	require.Equal(t, 1, row.SentCount)
}

func TestIndexObservationPostComparisonInputs_DeduplicatesAndCounts(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	detected := published.Add(10 * time.Second)
	detectedLater := published.Add(20 * time.Second)

	inputs := []ObservationPostComparisonInput{
		{
			Kind:              domain.OutboxKindCommunityPost,
			CanonicalPostID:   "community:post-1",
			ChannelID:         "UC_TEST",
			ActualPublishedAt: &published,
			DetectedAt:        &detected,
		},
		{
			Kind:            domain.OutboxKindCommunityPost,
			CanonicalPostID: "community:post-1",
			ChannelID:       "UC_TEST",
			DetectedAt:      &detectedLater,
		},
		{
			Kind:            domain.OutboxKindNewShort,
			CanonicalPostID: "short:video-1",
			ChannelID:       "UC_SHORT",
			DetectedAt:      &detected,
		},
	}

	index, keys, dupCount := indexObservationPostComparisonInputs(inputs)
	require.Equal(t, 1, dupCount)
	require.Len(t, keys, 2)
	require.Len(t, index, 2)

	communityKey := observationPostComparisonKey{
		kind:            domain.OutboxKindCommunityPost,
		channelID:       "UC_TEST",
		canonicalPostID: "community:post-1",
	}
	acc, ok := index[communityKey]
	require.True(t, ok)
	require.Equal(t, 2, acc.count)
	require.NotNil(t, acc.representative.DetectedAt)
	require.Equal(t, detected, acc.representative.DetectedAt.UTC())
}

func TestFirstNonBlankObservationPostComparisonString_PreservesNonEmpty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello", firstNonBlankObservationPostComparisonString("hello", "world"))
	require.Equal(t, "world", firstNonBlankObservationPostComparisonString("", "world"))
	require.Equal(t, "world", firstNonBlankObservationPostComparisonString("  ", " world "))
	require.Equal(t, "", firstNonBlankObservationPostComparisonString("", ""))
	require.Equal(t, "  ", firstNonBlankObservationPostComparisonString("  ", "  "))
}

func TestFirstNonZeroObservationPostComparisonValue_ReturnsFirstNonZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, domain.OutboxKindCommunityPost,
		firstNonZeroObservationPostComparisonValue(domain.OutboxKindCommunityPost, domain.OutboxKindNewShort))
	require.Equal(t, domain.OutboxKindNewShort,
		firstNonZeroObservationPostComparisonValue(domain.OutboxKind(""), domain.OutboxKindNewShort))
	require.Equal(t, domain.OutboxKind(""),
		firstNonZeroObservationPostComparisonValue(domain.OutboxKind(""), domain.OutboxKind("")))
}
