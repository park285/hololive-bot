package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryTelemetryRepository_ListCommunityShortsDeliveryLogsSince_FiltersAndOrdersRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	communityPublishedAt := now.Add(-2 * time.Hour)
	communityDetectedAt := now.Add(-119 * time.Minute)
	communityEventAt := now.Add(-118 * time.Minute)
	shortDetectedAt := now.Add(-30 * time.Minute)
	shortEventAt := now.Add(-29 * time.Minute)
	fallbackEventAt := now.Add(-10 * time.Minute)
	oldPublishedAt := now.Add(-30 * time.Hour)
	oldDetectedAt := now.Add(-29 * time.Hour)
	oldEventAt := now.Add(-29 * time.Hour)
	livePublishedAt := now.Add(-15 * time.Minute)
	liveDetectedAt := now.Add(-14 * time.Minute)
	liveEventAt := now.Add(-13 * time.Minute)

	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:        101,
			AttemptOrdinal:    1,
			OutboxID:          201,
			ChannelID:         "UC_COMMUNITY",
			ContentID:         "post-community",
			PostID:            "post-community",
			RoomID:            "room-community",
			AlarmType:         string(domain.AlarmTypeCommunity),
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        &communityDetectedAt,
			DedupeKey:         "youtube-notification:COMMUNITY_POST:post-community",
			DeliveryPath:      communityShortsDeliveryPath,
			DeliveryMode:      "grouped",
			SendResult:        "success",
			EventAt:           communityEventAt,
			NextAttemptAt:     communityEventAt,
		},
		{
			DeliveryID:     102,
			AttemptOrdinal: 1,
			OutboxID:       202,
			ChannelID:      "UC_SHORT",
			ContentID:      "short-recent",
			PostID:         "short-recent",
			RoomID:         "room-short",
			AlarmType:      string(domain.AlarmTypeShorts),
			DetectedAt:     &shortDetectedAt,
			DedupeKey:      "youtube-notification:NEW_SHORT:short-recent",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "failure",
			FailureReason:  "retry",
			EventAt:        shortEventAt,
			NextAttemptAt:  shortEventAt,
		},
		{
			DeliveryID:     103,
			AttemptOrdinal: 1,
			OutboxID:       203,
			ChannelID:      "UC_FALLBACK",
			ContentID:      "fallback-event-only",
			PostID:         "fallback-event-only",
			RoomID:         "room-fallback",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:fallback-event-only",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        fallbackEventAt,
			NextAttemptAt:  fallbackEventAt,
		},
		{
			DeliveryID:        104,
			AttemptOrdinal:    1,
			OutboxID:          204,
			ChannelID:         "UC_OLD",
			ContentID:         "post-old",
			PostID:            "post-old",
			RoomID:            "room-old",
			AlarmType:         string(domain.AlarmTypeCommunity),
			ActualPublishedAt: &oldPublishedAt,
			DetectedAt:        &oldDetectedAt,
			DedupeKey:         "youtube-notification:COMMUNITY_POST:post-old",
			DeliveryPath:      communityShortsDeliveryPath,
			DeliveryMode:      "grouped",
			SendResult:        "success",
			EventAt:           oldEventAt,
			NextAttemptAt:     oldEventAt,
		},
		{
			DeliveryID:        105,
			AttemptOrdinal:    1,
			OutboxID:          205,
			ChannelID:         "UC_LIVE",
			ContentID:         "video-live",
			PostID:            "video-live",
			RoomID:            "room-live",
			AlarmType:         string(domain.AlarmTypeLive),
			ActualPublishedAt: &livePublishedAt,
			DetectedAt:        &liveDetectedAt,
			DedupeKey:         "youtube-notification:NEW_VIDEO:video-live",
			DeliveryPath:      communityShortsDeliveryPath,
			DeliveryMode:      "grouped",
			SendResult:        "success",
			EventAt:           liveEventAt,
			NextAttemptAt:     liveEventAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListCommunityShortsDeliveryLogsSince(ctx, windowStart, 0)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	require.Equal(t, int64(103), rows[0].DeliveryID)
	require.Equal(t, "fallback-event-only", rows[0].ContentID)
	require.Nil(t, rows[0].ActualPublishedAt)
	require.Nil(t, rows[0].DetectedAt)
	require.Equal(t, fallbackEventAt, rows[0].EventAt.UTC())

	require.Equal(t, int64(102), rows[1].DeliveryID)
	require.Equal(t, "short-recent", rows[1].ContentID)
	require.Nil(t, rows[1].ActualPublishedAt)
	require.NotNil(t, rows[1].DetectedAt)
	require.Equal(t, shortDetectedAt, rows[1].DetectedAt.UTC())

	require.Equal(t, int64(101), rows[2].DeliveryID)
	require.Equal(t, "post-community", rows[2].ContentID)
	require.NotNil(t, rows[2].ActualPublishedAt)
	require.Equal(t, communityPublishedAt, rows[2].ActualPublishedAt.UTC())
}

func TestDeliveryTelemetryRepository_ListCommunityShortsDeliveryLogsSince_RespectsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	rows := []deliveryTelemetryTestBufferModel{}
	for i := range 3 {
		publishedAt := now.Add(time.Duration(-(i + 1)) * time.Hour)
		eventAt := publishedAt.Add(time.Minute)
		rows = append(rows, deliveryTelemetryTestBufferModel{
			DeliveryID:        int64(200 + i),
			AttemptOrdinal:    1,
			OutboxID:          int64(300 + i),
			ChannelID:         "UC_LIMIT",
			ContentID:         publishedAt.Format(time.RFC3339),
			PostID:            publishedAt.Format(time.RFC3339),
			RoomID:            "room-limit",
			AlarmType:         string(domain.AlarmTypeCommunity),
			ActualPublishedAt: &publishedAt,
			DedupeKey:         "youtube-notification:COMMUNITY_POST:" + publishedAt.Format(time.RFC3339),
			DeliveryPath:      communityShortsDeliveryPath,
			DeliveryMode:      "grouped",
			SendResult:        "success",
			EventAt:           eventAt,
			NextAttemptAt:     eventAt,
		})
	}
	require.NoError(t, insertDeliveryTestRows(db, &rows).Error)

	repository := NewDeliveryTelemetryRepository(db)
	limited, err := repository.ListCommunityShortsDeliveryLogsSince(ctx, windowStart, 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	require.Equal(t, rows[0].ContentID, limited[0].ContentID)
	require.Equal(t, rows[1].ContentID, limited[1].ContentID)
}
