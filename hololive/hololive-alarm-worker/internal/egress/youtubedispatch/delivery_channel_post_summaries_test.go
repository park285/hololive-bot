package youtubedispatch

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type channelPostSummaryTimes struct {
	now                         time.Time
	communitySuccessPublishedAt time.Time
	communitySuccessDetectedAt  time.Time
	communitySuccessSentAt      time.Time
	communitySuccessEventAt     time.Time
	shortFailurePublishedAt     time.Time
	shortFailureDetectedAt      time.Time
	shortFailureEventAt         time.Time
	recoveredPublishedAt        time.Time
	recoveredDetectedAt         time.Time
	recoveredSentAt             time.Time
	recoveredFailureEventAt     time.Time
	pendingDetectedAt           time.Time
	oldPublishedAt              time.Time
	oldDetectedAt               time.Time
	oldEventAt                  time.Time
}

type channelPostSummaryOutboxes struct {
	communitySuccess deliveryTelemetryTestOutboxModel
	shortFailure     deliveryTelemetryTestOutboxModel
	recovered        deliveryTelemetryTestOutboxModel
	old              deliveryTelemetryTestOutboxModel
}

func newChannelPostSummaryTimes(now time.Time) channelPostSummaryTimes {
	return channelPostSummaryTimes{
		now:                         now,
		communitySuccessPublishedAt: now.Add(-3 * time.Hour),
		communitySuccessDetectedAt:  now.Add(-175 * time.Minute),
		communitySuccessSentAt:      now.Add(-170 * time.Minute),
		communitySuccessEventAt:     now.Add(-170 * time.Minute),
		shortFailurePublishedAt:     now.Add(-2 * time.Hour),
		shortFailureDetectedAt:      now.Add(-115 * time.Minute),
		shortFailureEventAt:         now.Add(-110 * time.Minute),
		recoveredPublishedAt:        now.Add(-30 * time.Minute),
		recoveredDetectedAt:         now.Add(-29 * time.Minute),
		recoveredSentAt:             now.Add(-25 * time.Minute),
		recoveredFailureEventAt:     now.Add(-26 * time.Minute),
		pendingDetectedAt:           now.Add(-20 * time.Minute),
		oldPublishedAt:              now.Add(-30 * time.Hour),
		oldDetectedAt:               now.Add(-30*time.Hour + time.Minute),
		oldEventAt:                  now.Add(-29 * time.Hour),
	}
}

func TestDeliveryTelemetryRepository_ListChannelPostDeliverySummariesSince_AggregatesPerChannel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	times := newChannelPostSummaryTimes(time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC))
	windowStart := times.now.Add(-24 * time.Hour)
	outboxes := seedChannelPostSummaryOutboxes(t, db, times.now)

	seedChannelPostSummaryTracking(t, db, outboxes, times)
	require.NoError(t, insertDeliveryTestRows(db, slices.Concat(
		channelPostSummaryInWindowTelemetryRows(outboxes, times),
		channelPostSummaryStaleTelemetryRows(outboxes, times),
	)).Error)

	repository := telemetry.NewRepository(db)

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
	require.Equal(t, times.recoveredPublishedAt, *summaries[0].EarliestObservedAt)
	require.NotNil(t, summaries[0].LatestObservedAt)
	require.Equal(t, times.pendingDetectedAt, *summaries[0].LatestObservedAt)

	require.Equal(t, "UC_A", summaries[1].ChannelID)
	require.Equal(t, int64(2), summaries[1].DetectedPostCount)
	require.Equal(t, int64(2), summaries[1].AlarmSentPostCount)
	require.Equal(t, int64(1), summaries[1].SuccessPostCount)
	require.Equal(t, int64(1), summaries[1].FailedPostCount)
	require.Equal(t, int64(1), summaries[1].DetectedUnsentPostCount)
	require.Equal(t, int64(1), summaries[1].CommunityDetectedPostCount)
	require.Equal(t, int64(1), summaries[1].ShortsDetectedPostCount)
	require.NotNil(t, summaries[1].EarliestObservedAt)
	require.Equal(t, times.communitySuccessPublishedAt, *summaries[1].EarliestObservedAt)
	require.NotNil(t, summaries[1].LatestObservedAt)
	require.Equal(t, times.shortFailurePublishedAt, *summaries[1].LatestObservedAt)
}

func seedChannelPostSummaryOutboxes(t *testing.T, db *pgxpool.Pool, now time.Time) channelPostSummaryOutboxes {
	t.Helper()

	rows := seedDeliveryTelemetryOutboxes(t, db, now, []deliveryTelemetryOutboxSpec{
		{
			kind:      domain.OutboxKindCommunityPost,
			channelID: "UC_A",
			contentID: "community-success",
			payload:   `{"post_id":"community-success"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-176 * time.Minute),
		},
		{
			kind:         domain.OutboxKindNewShort,
			channelID:    "UC_A",
			contentID:    "short-failure",
			payload:      `{"video_id":"short-failure"}`,
			status:       domain.OutboxStatusPending,
			attemptCount: 2,
			createdAt:    now.Add(-116 * time.Minute),
		},
		{
			kind:         domain.OutboxKindCommunityPost,
			channelID:    "UC_B",
			contentID:    "community-recovered",
			payload:      `{"post_id":"community-recovered"}`,
			status:       domain.OutboxStatusSent,
			attemptCount: 1,
			createdAt:    now.Add(-29 * time.Minute),
		},
		{
			kind:      domain.OutboxKindCommunityPost,
			channelID: "UC_OLD",
			contentID: "community-old",
			payload:   `{"post_id":"community-old"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-30 * time.Hour),
		},
	})

	return channelPostSummaryOutboxes{
		communitySuccess: rows[0],
		shortFailure:     rows[1],
		recovered:        rows[2],
		old:              rows[3],
	}
}

func seedChannelPostSummaryTracking(
	t *testing.T,
	db *pgxpool.Pool,
	outboxes channelPostSummaryOutboxes,
	times channelPostSummaryTimes,
) {
	t.Helper()

	communitySuccessPublishedAt := times.communitySuccessPublishedAt
	communitySuccessSentAt := times.communitySuccessSentAt
	communitySuccessLatency := int64(communitySuccessSentAt.Sub(communitySuccessPublishedAt) / time.Millisecond)
	communitySuccessExceeded := true
	shortFailurePublishedAt := times.shortFailurePublishedAt
	recoveredPublishedAt := times.recoveredPublishedAt
	recoveredSentAt := times.recoveredSentAt
	recoveredLatency := int64(recoveredSentAt.Sub(recoveredPublishedAt) / time.Millisecond)
	recoveredExceeded := true
	oldPublishedAt := times.oldPublishedAt

	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestTrackingModel{
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            outboxes.communitySuccess.ContentID,
			ChannelID:            outboxes.communitySuccess.ChannelID,
			ActualPublishedAt:    &communitySuccessPublishedAt,
			DetectedAt:           times.communitySuccessDetectedAt,
			AlarmSentAt:          &communitySuccessSentAt,
			AlarmLatencyMillis:   &communitySuccessLatency,
			AlarmLatencyExceeded: &communitySuccessExceeded,
			CreatedAt:            times.now,
			UpdatedAt:            times.now,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         outboxes.shortFailure.ContentID,
			ChannelID:         outboxes.shortFailure.ChannelID,
			ActualPublishedAt: &shortFailurePublishedAt,
			DetectedAt:        times.shortFailureDetectedAt,
			CreatedAt:         times.now,
			UpdatedAt:         times.now,
		},
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            outboxes.recovered.ContentID,
			ChannelID:            outboxes.recovered.ChannelID,
			ActualPublishedAt:    &recoveredPublishedAt,
			DetectedAt:           times.recoveredDetectedAt,
			AlarmSentAt:          &recoveredSentAt,
			AlarmLatencyMillis:   &recoveredLatency,
			AlarmLatencyExceeded: &recoveredExceeded,
			CreatedAt:            times.now,
			UpdatedAt:            times.now,
		},
		{
			Kind:       string(domain.OutboxKindNewShort),
			ContentID:  "short-pending",
			ChannelID:  "UC_B",
			DetectedAt: times.pendingDetectedAt,
			CreatedAt:  times.now,
			UpdatedAt:  times.now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         outboxes.old.ContentID,
			ChannelID:         outboxes.old.ChannelID,
			ActualPublishedAt: &oldPublishedAt,
			DetectedAt:        times.oldDetectedAt,
			CreatedAt:         times.now,
			UpdatedAt:         times.now,
		},
	}).Error)
}

func channelPostSummaryInWindowTelemetryRows(
	outboxes channelPostSummaryOutboxes,
	times channelPostSummaryTimes,
) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.communitySuccess.ID,
			ChannelID:      outboxes.communitySuccess.ChannelID,
			ContentID:      outboxes.communitySuccess.ContentID,
			PostID:         outboxes.communitySuccess.ContentID,
			RoomID:         "room-a-success",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-success",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.communitySuccessEventAt,
			NextAttemptAt:  times.communitySuccessEventAt,
		},
		{
			DeliveryID:     2001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.shortFailure.ID,
			ChannelID:      outboxes.shortFailure.ChannelID,
			ContentID:      outboxes.shortFailure.ContentID,
			PostID:         outboxes.shortFailure.ContentID,
			RoomID:         "room-a-failure",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-failure",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModePerRoom,
			SendResult:     sendResultFailure,
			FailureReason:  deliveryReasonSendMessage,
			EventAt:        times.shortFailureEventAt,
			NextAttemptAt:  times.shortFailureEventAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.recovered.ID,
			ChannelID:      outboxes.recovered.ChannelID,
			ContentID:      outboxes.recovered.ContentID,
			PostID:         outboxes.recovered.ContentID,
			RoomID:         "room-b-retry",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-recovered",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultFailure,
			FailureReason:  "temporary",
			EventAt:        times.recoveredFailureEventAt,
			NextAttemptAt:  times.recoveredFailureEventAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 2,
			OutboxID:       outboxes.recovered.ID,
			ChannelID:      outboxes.recovered.ChannelID,
			ContentID:      outboxes.recovered.ContentID,
			PostID:         outboxes.recovered.ContentID,
			RoomID:         "room-b-retry",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-recovered",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.recoveredSentAt,
			NextAttemptAt:  times.recoveredSentAt,
		},
	}
}

func channelPostSummaryStaleTelemetryRows(
	outboxes channelPostSummaryOutboxes,
	times channelPostSummaryTimes,
) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     4001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.old.ID,
			ChannelID:      outboxes.old.ChannelID,
			ContentID:      outboxes.old.ContentID,
			PostID:         outboxes.old.ContentID,
			RoomID:         testRoomOld,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community-old",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.oldEventAt,
			NextAttemptAt:  times.oldEventAt,
		},
	}
}
