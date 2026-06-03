package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryTelemetryRepository_ListChannelPostDeliverySummariesSince_AggregatesPerChannel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	communitySuccessPublishedAt := now.Add(-3 * time.Hour)
	communitySuccessDetectedAt := now.Add(-175 * time.Minute)
	communitySuccessSentAt := now.Add(-170 * time.Minute)
	communitySuccessLatency := int64(communitySuccessSentAt.Sub(communitySuccessPublishedAt) / time.Millisecond)
	communitySuccessExceeded := true

	shortFailurePublishedAt := now.Add(-2 * time.Hour)
	shortFailureDetectedAt := now.Add(-115 * time.Minute)

	recoveredPublishedAt := now.Add(-30 * time.Minute)
	recoveredDetectedAt := now.Add(-29 * time.Minute)
	recoveredSentAt := now.Add(-25 * time.Minute)
	recoveredLatency := int64(recoveredSentAt.Sub(recoveredPublishedAt) / time.Millisecond)
	recoveredExceeded := true

	pendingDetectedAt := now.Add(-20 * time.Minute)

	oldPublishedAt := now.Add(-30 * time.Hour)
	oldDetectedAt := now.Add(-30*time.Hour + time.Minute)

	communitySuccessOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_A",
		ContentID:     "community-success",
		Payload:       `{"post_id":"community-success"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-176 * time.Minute),
	}
	shortFailureOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_A",
		ContentID:     "short-failure",
		Payload:       `{"video_id":"short-failure"}`,
		Status:        string(domain.OutboxStatusPending),
		AttemptCount:  2,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-116 * time.Minute),
	}
	recoveredOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_B",
		ContentID:     "community-recovered",
		Payload:       `{"post_id":"community-recovered"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  1,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-29 * time.Minute),
	}
	oldOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_OLD",
		ContentID:     "community-old",
		Payload:       `{"post_id":"community-old"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-30 * time.Hour),
	}
	require.NoError(t, db.Create(&communitySuccessOutbox).Error)
	require.NoError(t, db.Create(&shortFailureOutbox).Error)
	require.NoError(t, db.Create(&recoveredOutbox).Error)
	require.NoError(t, db.Create(&oldOutbox).Error)

	require.NoError(t, db.Create([]deliveryTelemetryTestTrackingModel{
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            communitySuccessOutbox.ContentID,
			ChannelID:            communitySuccessOutbox.ChannelID,
			ActualPublishedAt:    &communitySuccessPublishedAt,
			DetectedAt:           communitySuccessDetectedAt,
			AlarmSentAt:          &communitySuccessSentAt,
			AlarmLatencyMillis:   &communitySuccessLatency,
			AlarmLatencyExceeded: &communitySuccessExceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         shortFailureOutbox.ContentID,
			ChannelID:         shortFailureOutbox.ChannelID,
			ActualPublishedAt: &shortFailurePublishedAt,
			DetectedAt:        shortFailureDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            recoveredOutbox.ContentID,
			ChannelID:            recoveredOutbox.ChannelID,
			ActualPublishedAt:    &recoveredPublishedAt,
			DetectedAt:           recoveredDetectedAt,
			AlarmSentAt:          &recoveredSentAt,
			AlarmLatencyMillis:   &recoveredLatency,
			AlarmLatencyExceeded: &recoveredExceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:       string(domain.OutboxKindNewShort),
			ContentID:  "short-pending",
			ChannelID:  "UC_B",
			DetectedAt: pendingDetectedAt,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         oldOutbox.ContentID,
			ChannelID:         oldOutbox.ChannelID,
			ActualPublishedAt: &oldPublishedAt,
			DetectedAt:        oldDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}).Error)

	communitySuccessAt := now.Add(-170 * time.Minute)
	shortFailureAt := now.Add(-110 * time.Minute)
	recoveredFailureAt := now.Add(-26 * time.Minute)
	recoveredSuccessAt := now.Add(-25 * time.Minute)
	oldSuccessAt := now.Add(-29 * time.Hour)

	require.NoError(t, db.Create([]deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       communitySuccessOutbox.ID,
			ChannelID:      communitySuccessOutbox.ChannelID,
			ContentID:      communitySuccessOutbox.ContentID,
			PostID:         communitySuccessOutbox.ContentID,
			RoomID:         "room-a-success",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-success",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        communitySuccessAt,
			NextAttemptAt:  communitySuccessAt,
		},
		{
			DeliveryID:     2001,
			AttemptOrdinal: 1,
			OutboxID:       shortFailureOutbox.ID,
			ChannelID:      shortFailureOutbox.ChannelID,
			ContentID:      shortFailureOutbox.ContentID,
			PostID:         shortFailureOutbox.ContentID,
			RoomID:         "room-a-failure",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-failure",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "per_room",
			SendResult:     "failure",
			FailureReason:  "send message",
			EventAt:        shortFailureAt,
			NextAttemptAt:  shortFailureAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 1,
			OutboxID:       recoveredOutbox.ID,
			ChannelID:      recoveredOutbox.ChannelID,
			ContentID:      recoveredOutbox.ContentID,
			PostID:         recoveredOutbox.ContentID,
			RoomID:         "room-b-retry",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-recovered",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "failure",
			FailureReason:  "temporary",
			EventAt:        recoveredFailureAt,
			NextAttemptAt:  recoveredFailureAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 2,
			OutboxID:       recoveredOutbox.ID,
			ChannelID:      recoveredOutbox.ChannelID,
			ContentID:      recoveredOutbox.ContentID,
			PostID:         recoveredOutbox.ContentID,
			RoomID:         "room-b-retry",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-recovered",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        recoveredSuccessAt,
			NextAttemptAt:  recoveredSuccessAt,
		},
		{
			DeliveryID:     4001,
			AttemptOrdinal: 1,
			OutboxID:       oldOutbox.ID,
			ChannelID:      oldOutbox.ChannelID,
			ContentID:      oldOutbox.ContentID,
			PostID:         oldOutbox.ContentID,
			RoomID:         "room-old",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-old",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        oldSuccessAt,
			NextAttemptAt:  oldSuccessAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db.Pool)
	summaries, err := repository.ListChannelPostDeliverySummariesSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	require.Equal(t, "UC_B", summaries[0].ChannelID)
	require.Equal(t, int64(2), summaries[0].DetectedPostCount)
	require.Equal(t, int64(1), summaries[0].AlarmSentPostCount)
	require.Equal(t, int64(1), summaries[0].SuccessPostCount)
	require.Equal(t, int64(1), summaries[0].FailedPostCount)
	require.Equal(t, int64(1), summaries[0].DetectedUnsentPostCount)
	require.Equal(t, int64(1), summaries[0].CommunityDetectedPostCount)
	require.Equal(t, int64(1), summaries[0].ShortsDetectedPostCount)
	require.NotNil(t, summaries[0].EarliestObservedAt)
	require.Equal(t, recoveredPublishedAt, *summaries[0].EarliestObservedAt)
	require.NotNil(t, summaries[0].LatestObservedAt)
	require.Equal(t, pendingDetectedAt, *summaries[0].LatestObservedAt)

	require.Equal(t, "UC_A", summaries[1].ChannelID)
	require.Equal(t, int64(2), summaries[1].DetectedPostCount)
	require.Equal(t, int64(2), summaries[1].AlarmSentPostCount)
	require.Equal(t, int64(1), summaries[1].SuccessPostCount)
	require.Equal(t, int64(1), summaries[1].FailedPostCount)
	require.Equal(t, int64(1), summaries[1].DetectedUnsentPostCount)
	require.Equal(t, int64(1), summaries[1].CommunityDetectedPostCount)
	require.Equal(t, int64(1), summaries[1].ShortsDetectedPostCount)
	require.NotNil(t, summaries[1].EarliestObservedAt)
	require.Equal(t, communitySuccessPublishedAt, *summaries[1].EarliestObservedAt)
	require.NotNil(t, summaries[1].LatestObservedAt)
	require.Equal(t, shortFailurePublishedAt, *summaries[1].LatestObservedAt)
}
