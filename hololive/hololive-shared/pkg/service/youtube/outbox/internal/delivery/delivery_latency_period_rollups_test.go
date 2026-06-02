package delivery

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildPostLatencyPeriodSummaries_AggregatesSpecifiedPeriods(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	withinPublishedAt := now.Add(-50 * time.Minute)
	withinDetectedAt := now.Add(-49 * time.Minute)
	withinSentAt := now.Add(-49 * time.Minute)
	withinLatencyMillis := int64(time.Minute / time.Millisecond)
	withinExceeded := false

	exceededPublishedAt := now.Add(-90 * time.Minute)
	exceededDetectedAt := now.Add(-10 * time.Minute)
	exceededSentAt := now.Add(-87 * time.Minute)
	exceededLatencyMillis := int64(3 * time.Minute / time.Millisecond)
	exceeded := true

	pendingPublishedAt := now.Add(-20 * time.Minute)
	pendingDetectedAt := now.Add(-19 * time.Minute)

	fallbackDetectedAt := now.Add(-30 * time.Minute)
	fallbackSentAt := now.Add(-29 * time.Minute)

	oldPublishedAt := now.Add(-30 * time.Hour)
	oldDetectedAt := now.Add(-30*time.Hour + time.Minute)
	oldSentAt := now.Add(-30*time.Hour + 2*time.Minute)
	oldLatencyMillis := int64(2 * time.Minute / time.Millisecond)
	oldExceeded := false

	summaries, err := BuildPostLatencyPeriodSummaries([]PostSendCount{
		{
			AlarmType:            domain.AlarmTypeCommunity,
			ContentID:            "community-within",
			ActualPublishedAt:    &withinPublishedAt,
			DetectedAt:           &withinDetectedAt,
			AlarmSentAt:          &withinSentAt,
			AlarmLatencyMillis:   &withinLatencyMillis,
			AlarmLatencyExceeded: &withinExceeded,
		},
		{
			AlarmType:            domain.AlarmTypeShorts,
			ContentID:            "short-exceeded",
			ActualPublishedAt:    &exceededPublishedAt,
			DetectedAt:           &exceededDetectedAt,
			AlarmSentAt:          &exceededSentAt,
			AlarmLatencyMillis:   &exceededLatencyMillis,
			AlarmLatencyExceeded: &exceeded,
		},
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ContentID:         "community-pending",
			ActualPublishedAt: &pendingPublishedAt,
			DetectedAt:        &pendingDetectedAt,
		},
		{
			AlarmType:   domain.AlarmTypeShorts,
			ContentID:   "short-fallback",
			DetectedAt:  &fallbackDetectedAt,
			AlarmSentAt: &fallbackSentAt,
		},
		{
			AlarmType:            domain.AlarmTypeCommunity,
			ContentID:            "community-old",
			ActualPublishedAt:    &oldPublishedAt,
			DetectedAt:           &oldDetectedAt,
			AlarmSentAt:          &oldSentAt,
			AlarmLatencyMillis:   &oldLatencyMillis,
			AlarmLatencyExceeded: &oldExceeded,
		},
	}, []PostLatencyPeriod{
		{Label: "last_hour", StartAt: now.Add(-time.Hour), EndAt: now},
		{Label: "last_two_hours", StartAt: now.Add(-2 * time.Hour), EndAt: now},
	})
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	hourSummary := summaries[0]
	require.Equal(t, "last_hour", hourSummary.Label)
	require.Equal(t, int64(3), hourSummary.TotalPostCount)
	require.Equal(t, int64(2), hourSummary.AlarmSentPostCount)
	require.Equal(t, int64(1), hourSummary.PendingPostCount)
	require.Equal(t, int64(1), hourSummary.LatencyMeasuredPostCount)
	require.Equal(t, int64(1), hourSummary.WithinTargetPostCount)
	require.Equal(t, int64(0), hourSummary.ExceededPostCount)
	require.Equal(t, int64(2), hourSummary.CommunityPostCount)
	require.Equal(t, int64(0), hourSummary.CommunityExceededPostCount)
	require.Equal(t, int64(1), hourSummary.ShortsPostCount)
	require.Equal(t, int64(0), hourSummary.ShortsExceededPostCount)
	require.NotNil(t, hourSummary.AverageLatencyMillis)
	require.Equal(t, withinLatencyMillis, *hourSummary.AverageLatencyMillis)
	require.NotNil(t, hourSummary.P95LatencyMillis)
	require.Equal(t, withinLatencyMillis, *hourSummary.P95LatencyMillis)
	require.NotNil(t, hourSummary.MaxLatencyMillis)
	require.Equal(t, withinLatencyMillis, *hourSummary.MaxLatencyMillis)

	twoHourSummary := summaries[1]
	require.Equal(t, "last_two_hours", twoHourSummary.Label)
	require.Equal(t, int64(4), twoHourSummary.TotalPostCount)
	require.Equal(t, int64(3), twoHourSummary.AlarmSentPostCount)
	require.Equal(t, int64(1), twoHourSummary.PendingPostCount)
	require.Equal(t, int64(2), twoHourSummary.LatencyMeasuredPostCount)
	require.Equal(t, int64(1), twoHourSummary.WithinTargetPostCount)
	require.Equal(t, int64(1), twoHourSummary.ExceededPostCount)
	require.Equal(t, int64(2), twoHourSummary.CommunityPostCount)
	require.Equal(t, int64(0), twoHourSummary.CommunityExceededPostCount)
	require.Equal(t, int64(2), twoHourSummary.ShortsPostCount)
	require.Equal(t, int64(1), twoHourSummary.ShortsExceededPostCount)
	require.NotNil(t, twoHourSummary.AverageLatencyMillis)
	require.Equal(t, int64(2*time.Minute/time.Millisecond), *twoHourSummary.AverageLatencyMillis)
	require.NotNil(t, twoHourSummary.P95LatencyMillis)
	require.Equal(t, exceededLatencyMillis, *twoHourSummary.P95LatencyMillis)
	require.NotNil(t, twoHourSummary.MaxLatencyMillis)
	require.Equal(t, exceededLatencyMillis, *twoHourSummary.MaxLatencyMillis)
}

func TestBuildPostLatencyPeriodSummaries_ComputesDiscreteP95LatencyMillis(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	periods := []PostLatencyPeriod{{Label: "last_hour", StartAt: now.Add(-time.Hour), EndAt: now}}
	posts := make([]PostSendCount, 0, 20)
	withinTarget := false
	for i := 1; i <= 20; i++ {
		publishedAt := now.Add(-30 * time.Minute)
		detectedAt := publishedAt.Add(5 * time.Second)
		latencyMillis := int64(i)
		sentAt := publishedAt.Add(time.Duration(latencyMillis) * time.Millisecond)
		posts = append(posts, PostSendCount{
			AlarmType:            domain.AlarmTypeCommunity,
			ContentID:            fmt.Sprintf("community-%02d", i),
			ActualPublishedAt:    &publishedAt,
			DetectedAt:           &detectedAt,
			AlarmSentAt:          &sentAt,
			AlarmLatencyMillis:   &latencyMillis,
			AlarmLatencyExceeded: &withinTarget,
		})
	}

	summaries, err := BuildPostLatencyPeriodSummaries(posts, periods)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.NotNil(t, summaries[0].AverageLatencyMillis)
	require.Equal(t, int64(10), *summaries[0].AverageLatencyMillis)
	require.NotNil(t, summaries[0].P95LatencyMillis)
	require.Equal(t, int64(19), *summaries[0].P95LatencyMillis)
	require.NotNil(t, summaries[0].MaxLatencyMillis)
	require.Equal(t, int64(20), *summaries[0].MaxLatencyMillis)
}

func TestDeliveryTelemetryRepository_ListPostLatencyPeriodSummaries_UsesStoredPostResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	withinPublishedAt := now.Add(-45 * time.Minute)
	withinDetectedAt := now.Add(-44 * time.Minute)
	withinSentAt := now.Add(-43*time.Minute - 30*time.Second)
	withinLatencyMillis := int64(withinSentAt.Sub(withinPublishedAt) / time.Millisecond)
	withinExceeded := false

	exceededPublishedAt := now.Add(-80 * time.Minute)
	exceededDetectedAt := now.Add(-5 * time.Minute)
	exceededSentAt := now.Add(-75 * time.Minute)
	exceededLatencyMillis := int64(exceededSentAt.Sub(exceededPublishedAt) / time.Millisecond)
	exceeded := true

	fallbackDetectedAt := now.Add(-30 * time.Minute)
	fallbackSentAt := now.Add(-29 * time.Minute)

	pendingPublishedAt := now.Add(-20 * time.Minute)
	pendingDetectedAt := now.Add(-20 * time.Minute)

	oldPublishedAt := now.Add(-30 * time.Hour)
	oldDetectedAt := now.Add(-30*time.Hour + time.Minute)

	require.NoError(t, db.Create([]deliveryTelemetryTestTrackingModel{
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            "community-within",
			ChannelID:            "UC_community",
			ActualPublishedAt:    &withinPublishedAt,
			DetectedAt:           withinDetectedAt,
			AlarmSentAt:          &withinSentAt,
			AlarmLatencyMillis:   &withinLatencyMillis,
			AlarmLatencyExceeded: &withinExceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:                 string(domain.OutboxKindNewShort),
			ContentID:            "short-exceeded",
			ChannelID:            "UC_short",
			ActualPublishedAt:    &exceededPublishedAt,
			DetectedAt:           exceededDetectedAt,
			AlarmSentAt:          &exceededSentAt,
			AlarmLatencyMillis:   &exceededLatencyMillis,
			AlarmLatencyExceeded: &exceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:        string(domain.OutboxKindNewShort),
			ContentID:   "short-fallback",
			ChannelID:   "UC_short_fallback",
			DetectedAt:  fallbackDetectedAt,
			AlarmSentAt: &fallbackSentAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         "community-pending",
			ChannelID:         "UC_pending",
			ActualPublishedAt: &pendingPublishedAt,
			DetectedAt:        pendingDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         "short-old",
			ChannelID:         "UC_old",
			ActualPublishedAt: &oldPublishedAt,
			DetectedAt:        oldDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db.Pool)
	summaries, err := repository.ListPostLatencyPeriodSummaries(ctx, []PostLatencyPeriod{
		{Label: "last_hour", StartAt: now.Add(-time.Hour), EndAt: now},
		{Label: "last_day", StartAt: now.Add(-24 * time.Hour), EndAt: now},
	})
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	require.Equal(t, int64(3), summaries[0].TotalPostCount)
	require.Equal(t, int64(2), summaries[0].AlarmSentPostCount)
	require.Equal(t, int64(1), summaries[0].PendingPostCount)
	require.Equal(t, int64(1), summaries[0].LatencyMeasuredPostCount)
	require.Equal(t, int64(0), summaries[0].ExceededPostCount)
	require.Equal(t, int64(2), summaries[0].CommunityPostCount)
	require.Equal(t, int64(1), summaries[0].ShortsPostCount)
	require.NotNil(t, summaries[0].AverageLatencyMillis)
	require.Equal(t, withinLatencyMillis, *summaries[0].AverageLatencyMillis)
	require.NotNil(t, summaries[0].P95LatencyMillis)
	require.Equal(t, withinLatencyMillis, *summaries[0].P95LatencyMillis)

	require.Equal(t, int64(4), summaries[1].TotalPostCount)
	require.Equal(t, int64(3), summaries[1].AlarmSentPostCount)
	require.Equal(t, int64(1), summaries[1].PendingPostCount)
	require.Equal(t, int64(2), summaries[1].LatencyMeasuredPostCount)
	require.Equal(t, int64(1), summaries[1].WithinTargetPostCount)
	require.Equal(t, int64(1), summaries[1].ExceededPostCount)
	require.Equal(t, int64(2), summaries[1].CommunityPostCount)
	require.Equal(t, int64(2), summaries[1].ShortsPostCount)
	require.Equal(t, int64(1), summaries[1].ShortsExceededPostCount)
	require.NotNil(t, summaries[1].AverageLatencyMillis)
	require.Equal(t, int64((withinLatencyMillis+exceededLatencyMillis)/2), *summaries[1].AverageLatencyMillis)
	require.NotNil(t, summaries[1].P95LatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summaries[1].P95LatencyMillis)
	require.NotNil(t, summaries[1].MaxLatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summaries[1].MaxLatencyMillis)
}
