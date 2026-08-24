package youtubedispatch

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	analytics "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type postDeliveryPathUsageFixture struct {
	communityOutbox    deliveryTelemetryTestOutboxModel
	shortOutbox        deliveryTelemetryTestOutboxModel
	communitySuccessAt time.Time
	legacyTraceAt      time.Time
	shortSuccessAt     time.Time
}

type postDeliveryPathUsageKey struct {
	contentID    string
	deliveryPath string
}

func TestDeliveryTelemetryRepository_ListPostDeliveryPathUsageSince_GroupsByContentAndPath(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)
	fixture := seedPostDeliveryPathUsageFixture(t, db, now)

	require.NoError(t, insertDeliveryTestRows(db, postDeliveryPathUsageTelemetryRows(fixture)).Error)

	repository := telemetry.NewRepository(db)

	rows, err := repository.ListPostDeliveryPathUsageSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, rows, 4)

	byKey := postDeliveryPathUsageByKey(rows)
	communityID := fixture.communityOutbox.ContentID
	shortID := fixture.shortOutbox.ContentID

	communityNew, ok := byKey[postDeliveryPathUsageKey{contentID: communityID, deliveryPath: telemetry.CommunityShortsDeliveryPath}]
	require.True(t, ok)
	require.Equal(t, communityID, communityNew.PostID)
	require.Equal(t, int64(1), communityNew.SuccessSendCount)
	require.Equal(t, int64(1), communityNew.SuccessRoomCount)
	require.Equal(t, int64(0), communityNew.FailedAttemptCount)
	require.NotNil(t, communityNew.FirstSuccessAt)
	require.Equal(t, fixture.communitySuccessAt, *communityNew.FirstSuccessAt)

	communityLegacy, ok := byKey[postDeliveryPathUsageKey{contentID: communityID, deliveryPath: "legacy_alarm_queue"}]
	require.True(t, ok)
	require.Equal(t, communityID, communityLegacy.PostID)
	require.Equal(t, int64(0), communityLegacy.SuccessSendCount)
	require.Equal(t, int64(0), communityLegacy.SuccessRoomCount)
	require.Equal(t, int64(1), communityLegacy.FailedAttemptCount)
	require.NotNil(t, communityLegacy.LastEventAt)
	require.Equal(t, fixture.legacyTraceAt, *communityLegacy.LastEventAt)

	shortRow, ok := byKey[postDeliveryPathUsageKey{contentID: shortID, deliveryPath: telemetry.CommunityShortsDeliveryPath}]
	require.True(t, ok)
	require.Equal(t, shortID, shortRow.PostID)
	require.Equal(t, int64(1), shortRow.SuccessSendCount)
	require.Equal(t, int64(0), shortRow.FailedAttemptCount)

	zeroRow, ok := byKey[postDeliveryPathUsageKey{contentID: "post-route-zero", deliveryPath: ""}]
	require.True(t, ok)
	require.Equal(t, "post-route-zero", zeroRow.PostID)
	require.Equal(t, int64(0), zeroRow.SuccessSendCount)
	require.Equal(t, int64(0), zeroRow.FailedAttemptCount)
	require.Nil(t, zeroRow.FirstEventAt)
	require.Nil(t, zeroRow.LastEventAt)
}

func postDeliveryPathUsageByKey(rows []analytics.PostDeliveryPathUsage) map[postDeliveryPathUsageKey]analytics.PostDeliveryPathUsage {
	byKey := make(map[postDeliveryPathUsageKey]analytics.PostDeliveryPathUsage, len(rows))

	for i := range rows {
		byKey[postDeliveryPathUsageKey{contentID: rows[i].ContentID, deliveryPath: rows[i].DeliveryPath}] = rows[i]
	}

	return byKey
}

func seedPostDeliveryPathUsageFixture(t *testing.T, db *pgxpool.Pool, now time.Time) postDeliveryPathUsageFixture {
	t.Helper()

	communityPublishedAt := now.Add(-3 * time.Hour)
	communityDetectedAt := now.Add(-2 * time.Hour)
	shortPublishedAt := now.Add(-90 * time.Minute)
	shortDetectedAt := now.Add(-80 * time.Minute)
	zeroPublishedAt := now.Add(-70 * time.Minute)
	zeroDetectedAt := now.Add(-65 * time.Minute)

	fixture := postDeliveryPathUsageFixture{
		communityOutbox: deliveryTelemetryTestOutboxModel{
			Kind:          string(domain.OutboxKindCommunityPost),
			ChannelID:     "UC_route_community",
			ContentID:     "post-route-community",
			Payload:       `{"post_id":"post-route-community","content_text":"community"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: now,
			CreatedAt:     now.Add(-2 * time.Hour),
		},
		shortOutbox: deliveryTelemetryTestOutboxModel{
			Kind:          string(domain.OutboxKindNewShort),
			ChannelID:     "UC_route_short",
			ContentID:     "short-route",
			Payload:       `{"video_id":"short-route","title":"short"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: now,
			CreatedAt:     now.Add(-75 * time.Minute),
		},
		communitySuccessAt: now.Add(-110 * time.Minute),
		legacyTraceAt:      now.Add(-105 * time.Minute),
		shortSuccessAt:     now.Add(-45 * time.Minute),
	}

	require.NoError(t, insertDeliveryTestRows(db, &fixture.communityOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &fixture.shortOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         fixture.communityOutbox.ContentID,
			ChannelID:         fixture.communityOutbox.ChannelID,
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         fixture.shortOutbox.ContentID,
			ChannelID:         fixture.shortOutbox.ChannelID,
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         "post-route-zero",
			ChannelID:         "UC_route_zero",
			ActualPublishedAt: &zeroPublishedAt,
			DetectedAt:        zeroDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}).Error)

	return fixture
}

func postDeliveryPathUsageTelemetryRows(fixture postDeliveryPathUsageFixture) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       fixture.communityOutbox.ID,
			ChannelID:      fixture.communityOutbox.ChannelID,
			ContentID:      fixture.communityOutbox.ContentID,
			PostID:         fixture.communityOutbox.ContentID,
			RoomID:         testRoomCommunity,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-route-community",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        fixture.communitySuccessAt,
			NextAttemptAt:  fixture.communitySuccessAt,
		},
		{
			DeliveryID:     1002,
			AttemptOrdinal: 1,
			OutboxID:       fixture.communityOutbox.ID,
			ChannelID:      fixture.communityOutbox.ChannelID,
			ContentID:      fixture.communityOutbox.ContentID,
			PostID:         fixture.communityOutbox.ContentID,
			RoomID:         "room-community-legacy",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-route-community",
			DeliveryPath:   "legacy_alarm_queue",
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultFailure,
			FailureReason:  "blocked",
			EventAt:        fixture.legacyTraceAt,
			NextAttemptAt:  fixture.legacyTraceAt,
		},
		{
			DeliveryID:     2001,
			AttemptOrdinal: 1,
			OutboxID:       fixture.shortOutbox.ID,
			ChannelID:      fixture.shortOutbox.ChannelID,
			ContentID:      fixture.shortOutbox.ContentID,
			PostID:         fixture.shortOutbox.ContentID,
			RoomID:         testRoomShort,
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-route",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModePerRoom,
			SendResult:     sendResultSuccess,
			EventAt:        fixture.shortSuccessAt,
			NextAttemptAt:  fixture.shortSuccessAt,
		},
	}
}
