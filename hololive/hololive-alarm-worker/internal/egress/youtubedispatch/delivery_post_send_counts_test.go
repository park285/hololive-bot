package youtubedispatch

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	analytics "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type deliveryTelemetryTestTrackingModel struct {
	Kind                        string `db:"kind"`
	ContentID                   string `db:"content_id"`
	CanonicalContentID          string
	ChannelID                   string `db:"channel_id"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `db:"detected_at"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `db:"delivery_status"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (deliveryTelemetryTestTrackingModel) TableName() string {
	return testTableContentAlarmTracking
}

type postSendCountOutboxes struct {
	community deliveryTelemetryTestOutboxModel
	short     deliveryTelemetryTestOutboxModel
	old       deliveryTelemetryTestOutboxModel
	nonTarget deliveryTelemetryTestOutboxModel
}

type postSendCountTimes struct {
	now                           time.Time
	communityPublishedAt          time.Time
	communityDetectedAt           time.Time
	communityAlarmSentAt          time.Time
	communityAlarmLatencyMillis   int64
	communityAlarmLatencyExceeded bool
	communityFirstSuccessAt       time.Time
	communitySecondSuccessAt      time.Time
	communityFailureAt            time.Time
	shortPublishedAt              time.Time
	shortDetectedAt               time.Time
	shortAlarmSentAt              time.Time
	shortAlarmLatencyMillis       int64
	shortAlarmLatencyExceeded     bool
	shortSuccessAt                time.Time
	zeroPublishedAt               time.Time
	zeroDetectedAt                time.Time
	oldPublishedAt                time.Time
	oldDetectedAt                 time.Time
	oldRecentSuccessAt            time.Time
	nonTargetPublishedAt          time.Time
	nonTargetDetectedAt           time.Time
	nonTargetSuccessAt            time.Time
}

func newPostSendCountTimes(now time.Time) postSendCountTimes {
	communityPublishedAt := now.Add(-3 * time.Hour)
	communityAlarmSentAt := now.Add(-108 * time.Minute)
	shortPublishedAt := now.Add(-90 * time.Minute)
	shortAlarmSentAt := now.Add(-44 * time.Minute)

	return postSendCountTimes{
		now:                           now,
		communityPublishedAt:          communityPublishedAt,
		communityDetectedAt:           now.Add(-2*time.Hour - 30*time.Minute),
		communityAlarmSentAt:          communityAlarmSentAt,
		communityAlarmLatencyMillis:   int64(communityAlarmSentAt.Sub(communityPublishedAt) / time.Millisecond),
		communityAlarmLatencyExceeded: true,
		communityFirstSuccessAt:       now.Add(-110 * time.Minute),
		communitySecondSuccessAt:      now.Add(-100 * time.Minute),
		communityFailureAt:            now.Add(-95 * time.Minute),
		shortPublishedAt:              shortPublishedAt,
		shortDetectedAt:               now.Add(-80 * time.Minute),
		shortAlarmSentAt:              shortAlarmSentAt,
		shortAlarmLatencyMillis:       int64(shortAlarmSentAt.Sub(shortPublishedAt) / time.Millisecond),
		shortAlarmLatencyExceeded:     true,
		shortSuccessAt:                now.Add(-45 * time.Minute),
		zeroPublishedAt:               now.Add(-70 * time.Minute),
		zeroDetectedAt:                now.Add(-65 * time.Minute),
		oldPublishedAt:                now.Add(-26 * time.Hour),
		oldDetectedAt:                 now.Add(-25 * time.Hour),
		oldRecentSuccessAt:            now.Add(-30 * time.Minute),
		nonTargetPublishedAt:          now.Add(-40 * time.Minute),
		nonTargetDetectedAt:           now.Add(-35 * time.Minute),
		nonTargetSuccessAt:            now.Add(-20 * time.Minute),
	}
}

func TestDeliveryTelemetryRepository_ListPostSendCountsSince_AggregatesPerPost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	times := newPostSendCountTimes(time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC))
	windowStart := times.now.Add(-24 * time.Hour)
	outboxes := seedPostSendCountOutboxes(t, db, times.now)

	seedPostSendCountTracking(t, db, outboxes, times)
	require.NoError(t, insertDeliveryTestRows(db, slices.Concat(
		postSendCountCommunityTelemetryRows(outboxes, times),
		postSendCountOtherTelemetryRows(outboxes, times),
	)).Error)

	repository := telemetry.NewRepository(db)

	summaries, err := repository.ListPostSendCountsSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, summaries, 3)

	byContentID := make(map[string]analytics.PostSendCount, len(summaries))
	for i := range summaries {
		byContentID[summaries[i].ContentID] = summaries[i]
	}

	communitySummary, ok := byContentID[outboxes.community.ContentID]
	require.True(t, ok)
	assertCommunityPostSendCount(t, communitySummary, outboxes.community, times)

	shortSummary, ok := byContentID[outboxes.short.ContentID]
	require.True(t, ok)
	assertShortPostSendCount(t, shortSummary, outboxes.short, times)

	zeroSummary, ok := byContentID["post-zero-send"]
	require.True(t, ok)
	assertZeroPostSendCount(t, zeroSummary, times)

	_, exists := byContentID[outboxes.old.ContentID]
	require.False(t, exists)

	_, exists = byContentID[outboxes.nonTarget.ContentID]
	require.False(t, exists)
}

func seedPostSendCountOutboxes(t *testing.T, db *pgxpool.Pool, now time.Time) postSendCountOutboxes {
	t.Helper()

	rows := seedDeliveryTelemetryOutboxes(t, db, now, []deliveryTelemetryOutboxSpec{
		{
			kind:      domain.OutboxKindCommunityPost,
			channelID: "UC_community",
			contentID: "post-community",
			payload:   `{"post_id":"post-community","content_text":"community body"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-2 * time.Hour),
		},
		{
			kind:      domain.OutboxKindNewShort,
			channelID: "UC_short",
			contentID: "short-video",
			payload:   `{"video_id":"short-video","title":"short title"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-75 * time.Minute),
		},
		{
			kind:      domain.OutboxKindCommunityPost,
			channelID: "UC_old",
			contentID: "post-old-window",
			payload:   `{"post_id":"post-old-window","content_text":"old body"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-25 * time.Hour),
		},
		{
			kind:      domain.OutboxKindNewVideo,
			channelID: "UC_video",
			contentID: "video-ignored",
			payload:   `{"video_id":"video-ignored","title":"ignored"}`,
			status:    domain.OutboxStatusSent,
			createdAt: now.Add(-30 * time.Minute),
		},
	})

	return postSendCountOutboxes{
		community: rows[0],
		short:     rows[1],
		old:       rows[2],
		nonTarget: rows[3],
	}
}

func seedPostSendCountTracking(
	t *testing.T,
	db *pgxpool.Pool,
	outboxes postSendCountOutboxes,
	times postSendCountTimes,
) {
	t.Helper()

	communityPublishedAt := times.communityPublishedAt
	communityAlarmSentAt := times.communityAlarmSentAt
	communityAlarmLatencyMillis := times.communityAlarmLatencyMillis
	communityAlarmLatencyExceeded := times.communityAlarmLatencyExceeded
	shortPublishedAt := times.shortPublishedAt
	shortAlarmSentAt := times.shortAlarmSentAt
	shortAlarmLatencyMillis := times.shortAlarmLatencyMillis
	shortAlarmLatencyExceeded := times.shortAlarmLatencyExceeded
	zeroPublishedAt := times.zeroPublishedAt
	oldPublishedAt := times.oldPublishedAt
	nonTargetPublishedAt := times.nonTargetPublishedAt

	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestTrackingModel{
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            outboxes.community.ContentID,
			ChannelID:            outboxes.community.ChannelID,
			ActualPublishedAt:    &communityPublishedAt,
			DetectedAt:           times.communityDetectedAt,
			AlarmSentAt:          &communityAlarmSentAt,
			AlarmLatencyMillis:   &communityAlarmLatencyMillis,
			AlarmLatencyExceeded: &communityAlarmLatencyExceeded,
			CreatedAt:            times.now,
			UpdatedAt:            times.now,
		},
		{
			Kind:                 string(domain.OutboxKindNewShort),
			ContentID:            outboxes.short.ContentID,
			ChannelID:            outboxes.short.ChannelID,
			ActualPublishedAt:    &shortPublishedAt,
			DetectedAt:           times.shortDetectedAt,
			AlarmSentAt:          &shortAlarmSentAt,
			AlarmLatencyMillis:   &shortAlarmLatencyMillis,
			AlarmLatencyExceeded: &shortAlarmLatencyExceeded,
			CreatedAt:            times.now,
			UpdatedAt:            times.now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         "post-zero-send",
			ChannelID:         "UC_zero",
			ActualPublishedAt: &zeroPublishedAt,
			DetectedAt:        times.zeroDetectedAt,
			CreatedAt:         times.now,
			UpdatedAt:         times.now,
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
		{
			Kind:              string(domain.OutboxKindNewVideo),
			ContentID:         outboxes.nonTarget.ContentID,
			ChannelID:         outboxes.nonTarget.ChannelID,
			ActualPublishedAt: &nonTargetPublishedAt,
			DetectedAt:        times.nonTargetDetectedAt,
			CreatedAt:         times.now,
			UpdatedAt:         times.now,
		},
	}).Error)
}

func postSendCountCommunityTelemetryRows(
	outboxes postSendCountOutboxes,
	times postSendCountTimes,
) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.community.ID,
			ChannelID:      outboxes.community.ChannelID,
			ContentID:      outboxes.community.ContentID,
			PostID:         outboxes.community.ContentID,
			RoomID:         testRoomA,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      testDedupeKeyCommunityPost,
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.communityFirstSuccessAt,
			NextAttemptAt:  times.communityFirstSuccessAt,
		},
		{
			DeliveryID:     1001,
			AttemptOrdinal: 2,
			OutboxID:       outboxes.community.ID,
			ChannelID:      outboxes.community.ChannelID,
			ContentID:      outboxes.community.ContentID,
			PostID:         outboxes.community.ContentID,
			RoomID:         testRoomA,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      testDedupeKeyCommunityPost,
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.communitySecondSuccessAt,
			NextAttemptAt:  times.communitySecondSuccessAt,
		},
		{
			DeliveryID:     1002,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.community.ID,
			ChannelID:      outboxes.community.ChannelID,
			ContentID:      outboxes.community.ContentID,
			PostID:         outboxes.community.ContentID,
			RoomID:         "room-b",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      testDedupeKeyCommunityPost,
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.communitySecondSuccessAt,
			NextAttemptAt:  times.communitySecondSuccessAt,
		},
		{
			DeliveryID:     1003,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.community.ID,
			ChannelID:      outboxes.community.ChannelID,
			ContentID:      outboxes.community.ContentID,
			PostID:         outboxes.community.ContentID,
			RoomID:         "room-c",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      testDedupeKeyCommunityPost,
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultFailure,
			FailureReason:  deliveryReasonSendMessage,
			EventAt:        times.communityFailureAt,
			NextAttemptAt:  times.communityFailureAt,
		},
	}
}

func postSendCountOtherTelemetryRows(
	outboxes postSendCountOutboxes,
	times postSendCountTimes,
) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     2001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.short.ID,
			ChannelID:      outboxes.short.ChannelID,
			ContentID:      outboxes.short.ContentID,
			PostID:         outboxes.short.ContentID,
			RoomID:         testRoomShort,
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-video",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModePerRoom,
			SendResult:     sendResultSuccess,
			EventAt:        times.shortSuccessAt,
			NextAttemptAt:  times.shortSuccessAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.old.ID,
			ChannelID:      outboxes.old.ChannelID,
			ContentID:      outboxes.old.ContentID,
			PostID:         outboxes.old.ContentID,
			RoomID:         testRoomOld,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-old-window",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        times.oldRecentSuccessAt,
			NextAttemptAt:  times.oldRecentSuccessAt,
		},
		{
			DeliveryID:     4001,
			AttemptOrdinal: 1,
			OutboxID:       outboxes.nonTarget.ID,
			ChannelID:      outboxes.nonTarget.ChannelID,
			ContentID:      outboxes.nonTarget.ContentID,
			PostID:         outboxes.nonTarget.ContentID,
			RoomID:         "room-video",
			AlarmType:      string(domain.AlarmTypeLive),
			DedupeKey:      "youtube-notification:NEW_VIDEO:video-ignored",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModePerRoom,
			SendResult:     sendResultSuccess,
			EventAt:        times.nonTargetSuccessAt,
			NextAttemptAt:  times.nonTargetSuccessAt,
		},
	}
}

func assertCommunityPostSendCount(
	t *testing.T,
	summary analytics.PostSendCount,
	outbox deliveryTelemetryTestOutboxModel,
	times postSendCountTimes,
) {
	t.Helper()

	require.Equal(t, domain.OutboxKindCommunityPost, summary.OutboxKind)
	require.Equal(t, domain.AlarmTypeCommunity, summary.AlarmType)
	require.Equal(t, outbox.ChannelID, summary.ChannelID)
	require.Equal(t, outbox.ContentID, summary.PostID)
	require.Equal(t, int64(1), summary.OutboxCount)
	require.Equal(t, int64(3), summary.SuccessSendCount)
	require.Equal(t, int64(2), summary.SuccessRoomCount)
	require.Equal(t, int64(1), summary.DuplicateSuccessCount)
	require.Equal(t, int64(1), summary.FailedAttemptCount)
	require.NotNil(t, summary.FirstEventAt)
	require.Equal(t, times.communityFirstSuccessAt, *summary.FirstEventAt)
	require.NotNil(t, summary.LastEventAt)
	require.Equal(t, times.communityFailureAt, *summary.LastEventAt)
	require.NotNil(t, summary.FirstSuccessAt)
	require.Equal(t, times.communityFirstSuccessAt, *summary.FirstSuccessAt)
	require.NotNil(t, summary.LastSuccessAt)
	require.Equal(t, times.communitySecondSuccessAt, *summary.LastSuccessAt)
	require.NotNil(t, summary.ActualPublishedAt)
	require.Equal(t, times.communityPublishedAt, *summary.ActualPublishedAt)
	require.NotNil(t, summary.DetectedAt)
	require.Equal(t, times.communityDetectedAt, *summary.DetectedAt)
	require.NotNil(t, summary.AlarmSentAt)
	require.Equal(t, times.communityAlarmSentAt, *summary.AlarmSentAt)
	require.NotNil(t, summary.AlarmLatencyMillis)
	require.Equal(t, int64(72*time.Minute/time.Millisecond), *summary.AlarmLatencyMillis)
	require.NotNil(t, summary.AlarmLatencyExceeded)
	require.True(t, *summary.AlarmLatencyExceeded)
}

func assertShortPostSendCount(
	t *testing.T,
	summary analytics.PostSendCount,
	outbox deliveryTelemetryTestOutboxModel,
	times postSendCountTimes,
) {
	t.Helper()

	require.Equal(t, domain.OutboxKindNewShort, summary.OutboxKind)
	require.Equal(t, domain.AlarmTypeShorts, summary.AlarmType)
	require.Equal(t, outbox.ContentID, summary.PostID)
	require.Equal(t, int64(1), summary.OutboxCount)
	require.Equal(t, int64(1), summary.SuccessSendCount)
	require.Equal(t, int64(1), summary.SuccessRoomCount)
	require.Equal(t, int64(0), summary.DuplicateSuccessCount)
	require.Equal(t, int64(0), summary.FailedAttemptCount)
	require.NotNil(t, summary.FirstEventAt)
	require.Equal(t, times.shortSuccessAt, *summary.FirstEventAt)
	require.NotNil(t, summary.LastEventAt)
	require.Equal(t, times.shortSuccessAt, *summary.LastEventAt)
	require.NotNil(t, summary.FirstSuccessAt)
	require.Equal(t, times.shortSuccessAt, *summary.FirstSuccessAt)
	require.NotNil(t, summary.LastSuccessAt)
	require.Equal(t, times.shortSuccessAt, *summary.LastSuccessAt)
	require.NotNil(t, summary.ActualPublishedAt)
	require.Equal(t, times.shortPublishedAt, *summary.ActualPublishedAt)
	require.NotNil(t, summary.DetectedAt)
	require.Equal(t, times.shortDetectedAt, *summary.DetectedAt)
	require.NotNil(t, summary.AlarmSentAt)
	require.Equal(t, times.shortAlarmSentAt, *summary.AlarmSentAt)
	require.NotNil(t, summary.AlarmLatencyMillis)
	require.Equal(t, int64(46*time.Minute/time.Millisecond), *summary.AlarmLatencyMillis)
	require.NotNil(t, summary.AlarmLatencyExceeded)
	require.True(t, *summary.AlarmLatencyExceeded)
}

func assertZeroPostSendCount(t *testing.T, summary analytics.PostSendCount, times postSendCountTimes) {
	t.Helper()

	require.Equal(t, domain.OutboxKindCommunityPost, summary.OutboxKind)
	require.Equal(t, domain.AlarmTypeCommunity, summary.AlarmType)
	require.Equal(t, "UC_zero", summary.ChannelID)
	require.Equal(t, "post-zero-send", summary.PostID)
	require.Equal(t, int64(0), summary.OutboxCount)
	require.Equal(t, int64(0), summary.SuccessSendCount)
	require.Equal(t, int64(0), summary.SuccessRoomCount)
	require.Equal(t, int64(0), summary.DuplicateSuccessCount)
	require.Equal(t, int64(0), summary.FailedAttemptCount)
	require.Nil(t, summary.FirstEventAt)
	require.Nil(t, summary.LastEventAt)
	require.Nil(t, summary.FirstSuccessAt)
	require.Nil(t, summary.LastSuccessAt)
	require.NotNil(t, summary.ActualPublishedAt)
	require.Equal(t, times.zeroPublishedAt, *summary.ActualPublishedAt)
	require.NotNil(t, summary.DetectedAt)
	require.Equal(t, times.zeroDetectedAt, *summary.DetectedAt)
	require.Nil(t, summary.AlarmSentAt)
	require.Nil(t, summary.AlarmLatencyMillis)
	require.Nil(t, summary.AlarmLatencyExceeded)
}

type postSendCountWindowFixture struct {
	insideOutbox   deliveryTelemetryTestOutboxModel
	outsideOutbox  deliveryTelemetryTestOutboxModel
	insideEventAt  time.Time
	outsideEventAt time.Time
}

func TestDeliveryTelemetryRepository_ListPostSendCountsWithinPublishedWindow_AppliesUpperBound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	windowStart := time.Date(2026, time.April, 10, 10, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(45 * time.Minute)
	fixture := seedPostSendCountWindowFixture(t, db, windowStart, windowEnd)

	require.NoError(t, insertDeliveryTestRows(db, postSendCountWindowTelemetryRows(fixture)).Error)

	repository := telemetry.NewRepository(db)

	rows, err := repository.ListPostSendCountsWithinPublishedWindow(ctx, windowStart, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, fixture.insideOutbox.ContentID, rows[0].ContentID)
	require.Equal(t, int64(1), rows[0].SuccessSendCount)
}

func seedPostSendCountWindowFixture(
	t *testing.T,
	db *pgxpool.Pool,
	windowStart, windowEnd time.Time,
) postSendCountWindowFixture {
	t.Helper()

	insidePublishedAt := windowStart.Add(20 * time.Minute)
	insideDetectedAt := insidePublishedAt.Add(2 * time.Minute)
	outsidePublishedAt := windowEnd.Add(5 * time.Minute)
	outsideDetectedAt := outsidePublishedAt.Add(2 * time.Minute)

	fixture := postSendCountWindowFixture{
		insideOutbox: deliveryTelemetryTestOutboxModel{
			Kind:          string(domain.OutboxKindCommunityPost),
			ChannelID:     "UC_inside",
			ContentID:     "post-inside-window",
			Payload:       `{"post_id":"post-inside-window"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: insideDetectedAt.Add(1 * time.Minute),
			CreatedAt:     insideDetectedAt,
		},
		outsideOutbox: deliveryTelemetryTestOutboxModel{
			Kind:          string(domain.OutboxKindNewShort),
			ChannelID:     "UC_outside",
			ContentID:     "post-outside-window",
			Payload:       `{"video_id":"post-outside-window"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: outsideDetectedAt.Add(1 * time.Minute),
			CreatedAt:     outsideDetectedAt,
		},
		insideEventAt:  insideDetectedAt.Add(1 * time.Minute),
		outsideEventAt: outsideDetectedAt.Add(1 * time.Minute),
	}

	require.NoError(t, insertDeliveryTestRows(db, &fixture.insideOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &fixture.outsideOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         fixture.insideOutbox.ContentID,
			ChannelID:         fixture.insideOutbox.ChannelID,
			ActualPublishedAt: &insidePublishedAt,
			DetectedAt:        insideDetectedAt,
			CreatedAt:         insideDetectedAt,
			UpdatedAt:         insideDetectedAt,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         fixture.outsideOutbox.ContentID,
			ChannelID:         fixture.outsideOutbox.ChannelID,
			ActualPublishedAt: &outsidePublishedAt,
			DetectedAt:        outsideDetectedAt,
			CreatedAt:         outsideDetectedAt,
			UpdatedAt:         outsideDetectedAt,
		},
	}).Error)

	return fixture
}

func postSendCountWindowTelemetryRows(fixture postSendCountWindowFixture) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     9001,
			AttemptOrdinal: 1,
			OutboxID:       fixture.insideOutbox.ID,
			ChannelID:      fixture.insideOutbox.ChannelID,
			ContentID:      fixture.insideOutbox.ContentID,
			PostID:         fixture.insideOutbox.ContentID,
			RoomID:         "room-inside",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-inside-window",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        fixture.insideEventAt,
			NextAttemptAt:  fixture.insideEventAt,
		},
		{
			DeliveryID:     9002,
			AttemptOrdinal: 1,
			OutboxID:       fixture.outsideOutbox.ID,
			ChannelID:      fixture.outsideOutbox.ChannelID,
			ContentID:      fixture.outsideOutbox.ContentID,
			PostID:         fixture.outsideOutbox.ContentID,
			RoomID:         "room-outside",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:post-outside-window",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
			EventAt:        fixture.outsideEventAt,
			NextAttemptAt:  fixture.outsideEventAt,
		},
	}
}
