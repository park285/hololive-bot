package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryTelemetryRepository_ListPostDeliveryPathUsageSince_GroupsByContentAndPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)
	communityPublishedAt := now.Add(-3 * time.Hour)
	communityDetectedAt := now.Add(-2 * time.Hour)
	shortPublishedAt := now.Add(-90 * time.Minute)
	shortDetectedAt := now.Add(-80 * time.Minute)
	zeroPublishedAt := now.Add(-70 * time.Minute)
	zeroDetectedAt := now.Add(-65 * time.Minute)

	communityOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_route_community",
		ContentID:     "post-route-community",
		Payload:       `{"post_id":"post-route-community","content_text":"community"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-2 * time.Hour),
	}
	shortOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_route_short",
		ContentID:     "short-route",
		Payload:       `{"video_id":"short-route","title":"short"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-75 * time.Minute),
	}
	require.NoError(t, insertDeliveryTestRows(db, &communityOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &shortOutbox).Error)

	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         communityOutbox.ContentID,
			ChannelID:         communityOutbox.ChannelID,
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         shortOutbox.ContentID,
			ChannelID:         shortOutbox.ChannelID,
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

	communitySuccessAt := now.Add(-110 * time.Minute)
	legacyTraceAt := now.Add(-105 * time.Minute)
	shortSuccessAt := now.Add(-45 * time.Minute)

	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-community",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-route-community",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        communitySuccessAt,
			NextAttemptAt:  communitySuccessAt,
		},
		{
			DeliveryID:     1002,
			AttemptOrdinal: 1,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-community-legacy",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-route-community",
			DeliveryPath:   "legacy_alarm_queue",
			DeliveryMode:   "grouped",
			SendResult:     "failure",
			FailureReason:  "blocked",
			EventAt:        legacyTraceAt,
			NextAttemptAt:  legacyTraceAt,
		},
		{
			DeliveryID:     2001,
			AttemptOrdinal: 1,
			OutboxID:       shortOutbox.ID,
			ChannelID:      shortOutbox.ChannelID,
			ContentID:      shortOutbox.ContentID,
			PostID:         shortOutbox.ContentID,
			RoomID:         "room-short",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-route",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "per_room",
			SendResult:     "success",
			EventAt:        shortSuccessAt,
			NextAttemptAt:  shortSuccessAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListPostDeliveryPathUsageSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, rows, 4)

	type key struct {
		contentID    string
		deliveryPath string
	}
	byKey := make(map[key]PostDeliveryPathUsage, len(rows))
	for i := range rows {
		byKey[key{contentID: rows[i].ContentID, deliveryPath: rows[i].DeliveryPath}] = rows[i]
	}

	communityNew, ok := byKey[key{contentID: communityOutbox.ContentID, deliveryPath: communityShortsDeliveryPath}]
	require.True(t, ok)
	require.Equal(t, communityOutbox.ContentID, communityNew.PostID)
	require.Equal(t, int64(1), communityNew.SuccessSendCount)
	require.Equal(t, int64(1), communityNew.SuccessRoomCount)
	require.Equal(t, int64(0), communityNew.FailedAttemptCount)
	require.NotNil(t, communityNew.FirstSuccessAt)
	require.Equal(t, communitySuccessAt, *communityNew.FirstSuccessAt)

	communityLegacy, ok := byKey[key{contentID: communityOutbox.ContentID, deliveryPath: "legacy_alarm_queue"}]
	require.True(t, ok)
	require.Equal(t, communityOutbox.ContentID, communityLegacy.PostID)
	require.Equal(t, int64(0), communityLegacy.SuccessSendCount)
	require.Equal(t, int64(0), communityLegacy.SuccessRoomCount)
	require.Equal(t, int64(1), communityLegacy.FailedAttemptCount)
	require.NotNil(t, communityLegacy.LastEventAt)
	require.Equal(t, legacyTraceAt, *communityLegacy.LastEventAt)

	shortRow, ok := byKey[key{contentID: shortOutbox.ContentID, deliveryPath: communityShortsDeliveryPath}]
	require.True(t, ok)
	require.Equal(t, shortOutbox.ContentID, shortRow.PostID)
	require.Equal(t, int64(1), shortRow.SuccessSendCount)
	require.Equal(t, int64(0), shortRow.FailedAttemptCount)

	zeroRow, ok := byKey[key{contentID: "post-route-zero", deliveryPath: ""}]
	require.True(t, ok)
	require.Equal(t, "post-route-zero", zeroRow.PostID)
	require.Equal(t, int64(0), zeroRow.SuccessSendCount)
	require.Equal(t, int64(0), zeroRow.FailedAttemptCount)
	require.Nil(t, zeroRow.FirstEventAt)
	require.Nil(t, zeroRow.LastEventAt)
}
