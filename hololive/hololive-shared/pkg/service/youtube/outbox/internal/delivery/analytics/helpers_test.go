package analytics

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/stretchr/testify/require"
)

func TestCloneUTCTimePtr_NilReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, CloneUTCTimePtr(nil))
}

func TestCloneUTCTimePtr_NonZeroNormalizesToUTC(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("KST", 9*3600)
	in := time.Date(2026, 6, 10, 21, 0, 0, 0, loc)

	got := CloneUTCTimePtr(&in)
	require.NotNil(t, got)
	require.True(t, got.Equal(in))
	require.Equal(t, time.UTC, got.Location())
}

func TestCloneUTCTimePtr_ZeroTimeDivergesFromDeliverySQL(t *testing.T) {
	t.Parallel()

	var zero time.Time

	analyticsResult := CloneUTCTimePtr(&zero)
	require.NotNil(t, analyticsResult, "analytics clone returns non-nil for zero time")
	require.True(t, analyticsResult.IsZero())

	require.Nil(t, deliverysql.CloneUTCTimePtr(&zero), "deliverysql clone returns nil for zero time")
}

func TestBuildChannelPostDeliverySummaries_ZeroDetectedAtSurfacesZeroObservedAt(t *testing.T) {
	t.Parallel()

	var zero time.Time
	posts := []PostSendCount{
		{
			ChannelID:  "UC_alpha",
			AlarmType:  domain.AlarmTypeCommunity,
			DetectedAt: &zero,
		},
	}

	summaries, err := BuildChannelPostDeliverySummaries(posts)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	require.NotNil(t, summaries[0].EarliestObservedAt)
	require.True(t, summaries[0].EarliestObservedAt.IsZero())
	require.NotNil(t, summaries[0].LatestObservedAt)
	require.True(t, summaries[0].LatestObservedAt.IsZero())
}
