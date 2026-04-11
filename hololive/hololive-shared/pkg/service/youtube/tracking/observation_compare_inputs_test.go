package tracking

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildObservationPostComparisonInputsFromBaselines_NormalizesCanonicalMetadata(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := publishedAt.Add(14 * time.Second)

	inputs := BuildObservationPostComparisonInputsFromBaselines([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            " https://www.youtube.com/post/UgkxBaseline123?lc=1 ",
			ChannelID:         " UC_COMMUNITY ",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            " AbC123xyZ89 ",
			ChannelID:         " UC_SHORTS ",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		},
	})

	require.Len(t, inputs, 2)
	require.Equal(t, domain.OutboxKindCommunityPost, inputs[0].Kind)
	require.Equal(t, domain.AlarmTypeCommunity, inputs[0].AlarmType)
	require.Equal(t, "community:UgkxBaseline123", inputs[0].CanonicalPostID)
	require.Equal(t, "UC_COMMUNITY", inputs[0].ChannelID)
	require.Empty(t, inputs[0].ContentID)
	require.NotNil(t, inputs[0].ActualPublishedAt)
	require.Equal(t, publishedAt, inputs[0].ActualPublishedAt.UTC())
	require.NotNil(t, inputs[0].DetectedAt)
	require.Equal(t, detectedAt, inputs[0].DetectedAt.UTC())
	require.Nil(t, inputs[0].AlarmSentAt)

	require.Equal(t, domain.OutboxKindNewShort, inputs[1].Kind)
	require.Equal(t, domain.AlarmTypeShorts, inputs[1].AlarmType)
	require.Equal(t, "short:AbC123xyZ89", inputs[1].CanonicalPostID)
	require.Equal(t, "UC_SHORTS", inputs[1].ChannelID)
}

func TestBuildObservationPostComparisonInputsFromSentHistories_UsesSameCanonicalSchemaAsBaselines(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 2, 3, 4, 0, time.UTC)
	detectedAt := publishedAt.Add(9 * time.Second)
	alarmSentAt := publishedAt.Add(53 * time.Second)

	baselineInputs := BuildObservationPostComparisonInputsFromBaselines([]domain.YouTubeCommunityShortsObservationPostBaseline{{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "/post/UgkxShared123?lc=1",
		ChannelID:         " UC_SHARED ",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
	}})
	sentInputs := BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindCommunityPost, []ObservationAlarmSentHistoryRow{{
		PostID:            " ",
		ContentID:         "UgkxShared123",
		ChannelID:         " UC_SHARED ",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       alarmSentAt,
	}})

	require.Len(t, baselineInputs, 1)
	require.Len(t, sentInputs, 1)
	require.Equal(t, baselineInputs[0].Kind, sentInputs[0].Kind)
	require.Equal(t, baselineInputs[0].AlarmType, sentInputs[0].AlarmType)
	require.Equal(t, baselineInputs[0].CanonicalPostID, sentInputs[0].CanonicalPostID)
	require.Equal(t, baselineInputs[0].ChannelID, sentInputs[0].ChannelID)
	require.NotNil(t, sentInputs[0].AlarmSentAt)
	require.Equal(t, alarmSentAt, sentInputs[0].AlarmSentAt.UTC())

	row := sentInputs[0].ToObservationAlarmSentHistoryRow()
	require.Equal(t, "community:UgkxShared123", row.PostID)
	require.Equal(t, "UgkxShared123", row.ContentID)
	require.Equal(t, "UC_SHARED", row.ChannelID)
	require.Equal(t, detectedAt, row.DetectedAt.UTC())
	require.Equal(t, alarmSentAt, row.AlarmSentAt.UTC())
}
