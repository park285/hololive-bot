package youtubedispatch

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	analytics "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type postLatencyPeriodInputs struct {
	posts                 []analytics.PostSendCount
	withinLatencyMillis   int64
	exceededLatencyMillis int64
}

func newPostLatencyPeriodInputs(now time.Time) postLatencyPeriodInputs {
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

	return postLatencyPeriodInputs{
		posts: []analytics.PostSendCount{
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
		},
		withinLatencyMillis:   withinLatencyMillis,
		exceededLatencyMillis: exceededLatencyMillis,
	}
}

func TestBuildPostLatencyPeriodSummaries_AggregatesSpecifiedPeriods(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	inputs := newPostLatencyPeriodInputs(now)

	summaries, err := analytics.BuildPostLatencyPeriodSummaries(inputs.posts, []analytics.PostLatencyPeriod{
		{Label: "last_hour", StartAt: now.Add(-time.Hour), EndAt: now},
		{Label: "last_two_hours", StartAt: now.Add(-2 * time.Hour), EndAt: now},
	})
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	assertLastHourLatencySummary(t, summaries[0], inputs.withinLatencyMillis)
	assertLastTwoHoursLatencySummary(t, summaries[1], inputs.exceededLatencyMillis)
}

func assertLastHourLatencySummary(t *testing.T, summary analytics.PostLatencyPeriodSummary, withinLatencyMillis int64) {
	t.Helper()

	require.Equal(t, "last_hour", summary.Label)
	require.Equal(t, int64(3), summary.TotalPostCount)
	require.Equal(t, int64(2), summary.AlarmSentPostCount)
	require.Equal(t, int64(1), summary.PendingPostCount)
	require.Equal(t, int64(1), summary.LatencyMeasuredPostCount)
	require.Equal(t, int64(1), summary.WithinTargetPostCount)
	require.Equal(t, int64(0), summary.ExceededPostCount)
	require.Equal(t, int64(2), summary.CommunityPostCount)
	require.Equal(t, int64(0), summary.CommunityExceededPostCount)
	require.Equal(t, int64(1), summary.ShortsPostCount)
	require.Equal(t, int64(0), summary.ShortsExceededPostCount)
	require.NotNil(t, summary.AverageLatencyMillis)
	require.Equal(t, withinLatencyMillis, *summary.AverageLatencyMillis)
	require.NotNil(t, summary.P95LatencyMillis)
	require.Equal(t, withinLatencyMillis, *summary.P95LatencyMillis)
	require.NotNil(t, summary.MaxLatencyMillis)
	require.Equal(t, withinLatencyMillis, *summary.MaxLatencyMillis)
}

func assertLastTwoHoursLatencySummary(t *testing.T, summary analytics.PostLatencyPeriodSummary, exceededLatencyMillis int64) {
	t.Helper()

	require.Equal(t, "last_two_hours", summary.Label)
	require.Equal(t, int64(4), summary.TotalPostCount)
	require.Equal(t, int64(3), summary.AlarmSentPostCount)
	require.Equal(t, int64(1), summary.PendingPostCount)
	require.Equal(t, int64(2), summary.LatencyMeasuredPostCount)
	require.Equal(t, int64(1), summary.WithinTargetPostCount)
	require.Equal(t, int64(1), summary.ExceededPostCount)
	require.Equal(t, int64(2), summary.CommunityPostCount)
	require.Equal(t, int64(0), summary.CommunityExceededPostCount)
	require.Equal(t, int64(2), summary.ShortsPostCount)
	require.Equal(t, int64(1), summary.ShortsExceededPostCount)
	require.NotNil(t, summary.AverageLatencyMillis)
	require.Equal(t, int64(2*time.Minute/time.Millisecond), *summary.AverageLatencyMillis)
	require.NotNil(t, summary.P95LatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summary.P95LatencyMillis)
	require.NotNil(t, summary.MaxLatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summary.MaxLatencyMillis)
}

func TestBuildPostLatencyPeriodSummaries_ComputesDiscreteP95LatencyMillis(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	periods := []analytics.PostLatencyPeriod{{Label: "last_hour", StartAt: now.Add(-time.Hour), EndAt: now}}
	posts := make([]analytics.PostSendCount, 0, 20)
	withinTarget := false

	for i := 1; i <= 20; i++ {
		publishedAt := now.Add(-30 * time.Minute)
		detectedAt := publishedAt.Add(5 * time.Second)
		latencyMillis := int64(i)
		sentAt := publishedAt.Add(time.Duration(latencyMillis) * time.Millisecond)

		posts = append(posts, analytics.PostSendCount{
			AlarmType:            domain.AlarmTypeCommunity,
			ContentID:            fmt.Sprintf("community-%02d", i),
			ActualPublishedAt:    &publishedAt,
			DetectedAt:           &detectedAt,
			AlarmSentAt:          &sentAt,
			AlarmLatencyMillis:   &latencyMillis,
			AlarmLatencyExceeded: &withinTarget,
		})
	}

	summaries, err := analytics.BuildPostLatencyPeriodSummaries(posts, periods)
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

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	withinLatencyMillis, exceededLatencyMillis := seedPostLatencyPeriodTracking(t, db, now)

	repository := telemetry.NewRepository(db)

	summaries, err := repository.ListPostLatencyPeriodSummaries(ctx, []analytics.PostLatencyPeriod{
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
	require.Equal(t, (withinLatencyMillis+exceededLatencyMillis)/2, *summaries[1].AverageLatencyMillis)
	require.NotNil(t, summaries[1].P95LatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summaries[1].P95LatencyMillis)
	require.NotNil(t, summaries[1].MaxLatencyMillis)
	require.Equal(t, exceededLatencyMillis, *summaries[1].MaxLatencyMillis)
}

func seedPostLatencyPeriodTracking(t *testing.T, db *pgxpool.Pool, now time.Time) (int64, int64) {
	t.Helper()

	measured, withinLatencyMillis, exceededLatencyMillis := postLatencyPeriodMeasuredTrackingRows(now)
	rows := slices.Concat(measured, postLatencyPeriodUnmeasuredTrackingRows(now))

	require.NoError(t, insertDeliveryTestRows(db, rows).Error)

	return withinLatencyMillis, exceededLatencyMillis
}

func postLatencyPeriodMeasuredTrackingRows(now time.Time) ([]deliveryTelemetryTestTrackingModel, int64, int64) {
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

	return []deliveryTelemetryTestTrackingModel{
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
	}, withinLatencyMillis, exceededLatencyMillis
}

func postLatencyPeriodUnmeasuredTrackingRows(now time.Time) []deliveryTelemetryTestTrackingModel {
	fallbackDetectedAt := now.Add(-30 * time.Minute)
	fallbackSentAt := now.Add(-29 * time.Minute)
	pendingPublishedAt := now.Add(-20 * time.Minute)
	pendingDetectedAt := now.Add(-20 * time.Minute)
	oldPublishedAt := now.Add(-30 * time.Hour)
	oldDetectedAt := now.Add(-30*time.Hour + time.Minute)

	return []deliveryTelemetryTestTrackingModel{
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
	}
}
