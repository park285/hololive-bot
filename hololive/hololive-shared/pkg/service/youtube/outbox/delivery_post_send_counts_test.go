package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type sqliteTelemetryTrackingModel struct {
	Kind                        string `gorm:"primaryKey"`
	ContentID                   string `gorm:"primaryKey"`
	CanonicalContentID          string
	ChannelID                   string `gorm:"type:text;not null"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `gorm:"not null"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `gorm:"type:text;not null;default:'PENDING'"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (sqliteTelemetryTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

func TestDeliveryTelemetryRepository_ListPostSendCountsSince_AggregatesPerPost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteTelemetryOutboxModel{},
		&sqliteTelemetryBufferModel{},
		&sqliteTelemetryTrackingModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	communityPublishedAt := now.Add(-3 * time.Hour)
	communityDetectedAt := now.Add(-2*time.Hour - 30*time.Minute)
	shortPublishedAt := now.Add(-90 * time.Minute)
	shortDetectedAt := now.Add(-80 * time.Minute)
	communityAlarmSentAt := now.Add(-108 * time.Minute)
	communityAlarmLatencyMillis := int64(communityAlarmSentAt.Sub(communityPublishedAt) / time.Millisecond)
	communityAlarmLatencyExceeded := true
	shortAlarmSentAt := now.Add(-44 * time.Minute)
	shortAlarmLatencyMillis := int64(shortAlarmSentAt.Sub(shortPublishedAt) / time.Millisecond)
	shortAlarmLatencyExceeded := true
	zeroPublishedAt := now.Add(-70 * time.Minute)
	zeroDetectedAt := now.Add(-65 * time.Minute)
	oldPublishedAt := now.Add(-26 * time.Hour)
	oldDetectedAt := now.Add(-25 * time.Hour)
	nonTargetPublishedAt := now.Add(-40 * time.Minute)
	nonTargetDetectedAt := now.Add(-35 * time.Minute)

	communityOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_community",
		ContentID:     "post-community",
		Payload:       `{"post_id":"post-community","content_text":"community body"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-2 * time.Hour),
	}
	shortOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_short",
		ContentID:     "short-video",
		Payload:       `{"video_id":"short-video","title":"short title"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-75 * time.Minute),
	}
	oldOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_old",
		ContentID:     "post-old-window",
		Payload:       `{"post_id":"post-old-window","content_text":"old body"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-25 * time.Hour),
	}
	nonTargetOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindNewVideo),
		ChannelID:     "UC_video",
		ContentID:     "video-ignored",
		Payload:       `{"video_id":"video-ignored","title":"ignored"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-30 * time.Minute),
	}
	require.NoError(t, db.Create(&communityOutbox).Error)
	require.NoError(t, db.Create(&shortOutbox).Error)
	require.NoError(t, db.Create(&oldOutbox).Error)
	require.NoError(t, db.Create(&nonTargetOutbox).Error)

	require.NoError(t, db.Create([]sqliteTelemetryTrackingModel{
		{
			Kind:                 string(domain.OutboxKindCommunityPost),
			ContentID:            communityOutbox.ContentID,
			ChannelID:            communityOutbox.ChannelID,
			ActualPublishedAt:    &communityPublishedAt,
			DetectedAt:           communityDetectedAt,
			AlarmSentAt:          &communityAlarmSentAt,
			AlarmLatencyMillis:   &communityAlarmLatencyMillis,
			AlarmLatencyExceeded: &communityAlarmLatencyExceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:                 string(domain.OutboxKindNewShort),
			ContentID:            shortOutbox.ContentID,
			ChannelID:            shortOutbox.ChannelID,
			ActualPublishedAt:    &shortPublishedAt,
			DetectedAt:           shortDetectedAt,
			AlarmSentAt:          &shortAlarmSentAt,
			AlarmLatencyMillis:   &shortAlarmLatencyMillis,
			AlarmLatencyExceeded: &shortAlarmLatencyExceeded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         "post-zero-send",
			ChannelID:         "UC_zero",
			ActualPublishedAt: &zeroPublishedAt,
			DetectedAt:        zeroDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
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
		{
			Kind:              string(domain.OutboxKindNewVideo),
			ContentID:         nonTargetOutbox.ContentID,
			ChannelID:         nonTargetOutbox.ChannelID,
			ActualPublishedAt: &nonTargetPublishedAt,
			DetectedAt:        nonTargetDetectedAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}).Error)

	communityFirstSuccessAt := now.Add(-110 * time.Minute)
	communitySecondSuccessAt := now.Add(-100 * time.Minute)
	communityFailureAt := now.Add(-95 * time.Minute)
	shortSuccessAt := now.Add(-45 * time.Minute)
	oldRecentSuccessAt := now.Add(-30 * time.Minute)
	nonTargetSuccessAt := now.Add(-20 * time.Minute)

	rows := []sqliteTelemetryBufferModel{
		{
			DeliveryID:     1001,
			AttemptOrdinal: 1,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-a",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-community",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        communityFirstSuccessAt,
			NextAttemptAt:  communityFirstSuccessAt,
		},
		{
			DeliveryID:     1001,
			AttemptOrdinal: 2,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-a",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-community",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        communitySecondSuccessAt,
			NextAttemptAt:  communitySecondSuccessAt,
		},
		{
			DeliveryID:     1002,
			AttemptOrdinal: 1,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-b",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-community",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        communitySecondSuccessAt,
			NextAttemptAt:  communitySecondSuccessAt,
		},
		{
			DeliveryID:     1003,
			AttemptOrdinal: 1,
			OutboxID:       communityOutbox.ID,
			ChannelID:      communityOutbox.ChannelID,
			ContentID:      communityOutbox.ContentID,
			PostID:         communityOutbox.ContentID,
			RoomID:         "room-c",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-community",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "failure",
			FailureReason:  "send message",
			EventAt:        communityFailureAt,
			NextAttemptAt:  communityFailureAt,
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
			DedupeKey:      "youtube-notification:NEW_SHORT:short-video",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "per_room",
			SendResult:     "success",
			EventAt:        shortSuccessAt,
			NextAttemptAt:  shortSuccessAt,
		},
		{
			DeliveryID:     3001,
			AttemptOrdinal: 1,
			OutboxID:       oldOutbox.ID,
			ChannelID:      oldOutbox.ChannelID,
			ContentID:      oldOutbox.ContentID,
			PostID:         oldOutbox.ContentID,
			RoomID:         "room-old",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-old-window",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        oldRecentSuccessAt,
			NextAttemptAt:  oldRecentSuccessAt,
		},
		{
			DeliveryID:     4001,
			AttemptOrdinal: 1,
			OutboxID:       nonTargetOutbox.ID,
			ChannelID:      nonTargetOutbox.ChannelID,
			ContentID:      nonTargetOutbox.ContentID,
			PostID:         nonTargetOutbox.ContentID,
			RoomID:         "room-video",
			AlarmType:      string(domain.AlarmTypeLive),
			DedupeKey:      "youtube-notification:NEW_VIDEO:video-ignored",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "per_room",
			SendResult:     "success",
			EventAt:        nonTargetSuccessAt,
			NextAttemptAt:  nonTargetSuccessAt,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	repo := NewDeliveryTelemetryRepository(db)
	summaries, err := repo.ListPostSendCountsSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, summaries, 3)

	byContentID := make(map[string]PostSendCount, len(summaries))
	for i := range summaries {
		byContentID[summaries[i].ContentID] = summaries[i]
	}

	communitySummary, ok := byContentID[communityOutbox.ContentID]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindCommunityPost, communitySummary.OutboxKind)
	require.Equal(t, domain.AlarmTypeCommunity, communitySummary.AlarmType)
	require.Equal(t, communityOutbox.ChannelID, communitySummary.ChannelID)
	require.Equal(t, communityOutbox.ContentID, communitySummary.PostID)
	require.Equal(t, int64(1), communitySummary.OutboxCount)
	require.Equal(t, int64(3), communitySummary.SuccessSendCount)
	require.Equal(t, int64(2), communitySummary.SuccessRoomCount)
	require.Equal(t, int64(1), communitySummary.DuplicateSuccessCount)
	require.Equal(t, int64(1), communitySummary.FailedAttemptCount)
	require.NotNil(t, communitySummary.FirstEventAt)
	require.Equal(t, communityFirstSuccessAt, *communitySummary.FirstEventAt)
	require.NotNil(t, communitySummary.LastEventAt)
	require.Equal(t, communityFailureAt, *communitySummary.LastEventAt)
	require.NotNil(t, communitySummary.FirstSuccessAt)
	require.Equal(t, communityFirstSuccessAt, *communitySummary.FirstSuccessAt)
	require.NotNil(t, communitySummary.LastSuccessAt)
	require.Equal(t, communitySecondSuccessAt, *communitySummary.LastSuccessAt)
	require.NotNil(t, communitySummary.ActualPublishedAt)
	require.Equal(t, communityPublishedAt, *communitySummary.ActualPublishedAt)
	require.NotNil(t, communitySummary.DetectedAt)
	require.Equal(t, communityDetectedAt, *communitySummary.DetectedAt)
	require.NotNil(t, communitySummary.AlarmSentAt)
	require.Equal(t, communityAlarmSentAt, *communitySummary.AlarmSentAt)
	require.NotNil(t, communitySummary.AlarmLatencyMillis)
	require.Equal(t, int64(72*time.Minute/time.Millisecond), *communitySummary.AlarmLatencyMillis)
	require.NotNil(t, communitySummary.AlarmLatencyExceeded)
	require.True(t, *communitySummary.AlarmLatencyExceeded)

	shortSummary, ok := byContentID[shortOutbox.ContentID]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindNewShort, shortSummary.OutboxKind)
	require.Equal(t, domain.AlarmTypeShorts, shortSummary.AlarmType)
	require.Equal(t, shortOutbox.ContentID, shortSummary.PostID)
	require.Equal(t, int64(1), shortSummary.OutboxCount)
	require.Equal(t, int64(1), shortSummary.SuccessSendCount)
	require.Equal(t, int64(1), shortSummary.SuccessRoomCount)
	require.Equal(t, int64(0), shortSummary.DuplicateSuccessCount)
	require.Equal(t, int64(0), shortSummary.FailedAttemptCount)
	require.NotNil(t, shortSummary.FirstEventAt)
	require.Equal(t, shortSuccessAt, *shortSummary.FirstEventAt)
	require.NotNil(t, shortSummary.LastEventAt)
	require.Equal(t, shortSuccessAt, *shortSummary.LastEventAt)
	require.NotNil(t, shortSummary.FirstSuccessAt)
	require.Equal(t, shortSuccessAt, *shortSummary.FirstSuccessAt)
	require.NotNil(t, shortSummary.LastSuccessAt)
	require.Equal(t, shortSuccessAt, *shortSummary.LastSuccessAt)
	require.NotNil(t, shortSummary.ActualPublishedAt)
	require.Equal(t, shortPublishedAt, *shortSummary.ActualPublishedAt)
	require.NotNil(t, shortSummary.DetectedAt)
	require.Equal(t, shortDetectedAt, *shortSummary.DetectedAt)
	require.NotNil(t, shortSummary.AlarmSentAt)
	require.Equal(t, shortAlarmSentAt, *shortSummary.AlarmSentAt)
	require.NotNil(t, shortSummary.AlarmLatencyMillis)
	require.Equal(t, int64(46*time.Minute/time.Millisecond), *shortSummary.AlarmLatencyMillis)
	require.NotNil(t, shortSummary.AlarmLatencyExceeded)
	require.True(t, *shortSummary.AlarmLatencyExceeded)

	zeroSummary, ok := byContentID["post-zero-send"]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindCommunityPost, zeroSummary.OutboxKind)
	require.Equal(t, domain.AlarmTypeCommunity, zeroSummary.AlarmType)
	require.Equal(t, "UC_zero", zeroSummary.ChannelID)
	require.Equal(t, "post-zero-send", zeroSummary.PostID)
	require.Equal(t, int64(0), zeroSummary.OutboxCount)
	require.Equal(t, int64(0), zeroSummary.SuccessSendCount)
	require.Equal(t, int64(0), zeroSummary.SuccessRoomCount)
	require.Equal(t, int64(0), zeroSummary.DuplicateSuccessCount)
	require.Equal(t, int64(0), zeroSummary.FailedAttemptCount)
	require.Nil(t, zeroSummary.FirstEventAt)
	require.Nil(t, zeroSummary.LastEventAt)
	require.Nil(t, zeroSummary.FirstSuccessAt)
	require.Nil(t, zeroSummary.LastSuccessAt)
	require.NotNil(t, zeroSummary.ActualPublishedAt)
	require.Equal(t, zeroPublishedAt, *zeroSummary.ActualPublishedAt)
	require.NotNil(t, zeroSummary.DetectedAt)
	require.Equal(t, zeroDetectedAt, *zeroSummary.DetectedAt)
	require.Nil(t, zeroSummary.AlarmSentAt)
	require.Nil(t, zeroSummary.AlarmLatencyMillis)
	require.Nil(t, zeroSummary.AlarmLatencyExceeded)

	_, exists := byContentID[oldOutbox.ContentID]
	require.False(t, exists)
	_, exists = byContentID[nonTargetOutbox.ContentID]
	require.False(t, exists)
}

func TestDeliveryTelemetryRepository_ListPostSendCountsWithinPublishedWindow_AppliesUpperBound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteTelemetryOutboxModel{},
		&sqliteTelemetryBufferModel{},
		&sqliteTelemetryTrackingModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	windowStart := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(45 * time.Minute)
	insidePublishedAt := windowStart.Add(20 * time.Minute)
	insideDetectedAt := insidePublishedAt.Add(2 * time.Minute)
	insideEventAt := insideDetectedAt.Add(1 * time.Minute)
	outsidePublishedAt := windowEnd.Add(5 * time.Minute)
	outsideDetectedAt := outsidePublishedAt.Add(2 * time.Minute)
	outsideEventAt := outsideDetectedAt.Add(1 * time.Minute)

	insideOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_inside",
		ContentID:     "post-inside-window",
		Payload:       `{"post_id":"post-inside-window"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: insideEventAt,
		CreatedAt:     insideDetectedAt,
	}
	outsideOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_outside",
		ContentID:     "post-outside-window",
		Payload:       `{"video_id":"post-outside-window"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: outsideEventAt,
		CreatedAt:     outsideDetectedAt,
	}
	require.NoError(t, db.Create(&insideOutbox).Error)
	require.NoError(t, db.Create(&outsideOutbox).Error)

	require.NoError(t, db.Create([]sqliteTelemetryTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         insideOutbox.ContentID,
			ChannelID:         insideOutbox.ChannelID,
			ActualPublishedAt: &insidePublishedAt,
			DetectedAt:        insideDetectedAt,
			CreatedAt:         insideDetectedAt,
			UpdatedAt:         insideDetectedAt,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         outsideOutbox.ContentID,
			ChannelID:         outsideOutbox.ChannelID,
			ActualPublishedAt: &outsidePublishedAt,
			DetectedAt:        outsideDetectedAt,
			CreatedAt:         outsideDetectedAt,
			UpdatedAt:         outsideDetectedAt,
		},
	}).Error)

	require.NoError(t, db.Create([]sqliteTelemetryBufferModel{
		{
			DeliveryID:     9001,
			AttemptOrdinal: 1,
			OutboxID:       insideOutbox.ID,
			ChannelID:      insideOutbox.ChannelID,
			ContentID:      insideOutbox.ContentID,
			PostID:         insideOutbox.ContentID,
			RoomID:         "room-inside",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-inside-window",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        insideEventAt,
			NextAttemptAt:  insideEventAt,
		},
		{
			DeliveryID:     9002,
			AttemptOrdinal: 1,
			OutboxID:       outsideOutbox.ID,
			ChannelID:      outsideOutbox.ChannelID,
			ContentID:      outsideOutbox.ContentID,
			PostID:         outsideOutbox.ContentID,
			RoomID:         "room-outside",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:post-outside-window",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        outsideEventAt,
			NextAttemptAt:  outsideEventAt,
		},
	}).Error)

	repo := NewDeliveryTelemetryRepository(db)
	rows, err := repo.ListPostSendCountsWithinPublishedWindow(ctx, windowStart, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, insideOutbox.ContentID, rows[0].ContentID)
	require.Equal(t, int64(1), rows[0].SuccessSendCount)
}

func TestDeliveryTelemetryRepository_ListPostSendCountsWithinObservationWindow_ExcludesLateDetections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteTelemetryOutboxModel{},
		&sqliteTelemetryBufferModel{},
		&sqliteTelemetryTrackingModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)

	timelyOutbox := sqliteTelemetryOutboxModel{
		ID:            9101,
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC-timely",
		ContentID:     "post-timely",
		Payload:       `{"post_id":"post-timely"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  1,
		NextAttemptAt: windowStart,
		CreatedAt:     windowStart.Add(3 * time.Minute),
	}
	lateOutbox := sqliteTelemetryOutboxModel{
		ID:            9102,
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC-late",
		ContentID:     "short-late",
		Payload:       `{"post_id":"short-late"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  1,
		NextAttemptAt: windowStart,
		CreatedAt:     windowStart.Add(10 * time.Minute),
	}
	require.NoError(t, db.Create([]sqliteTelemetryOutboxModel{timelyOutbox, lateOutbox}).Error)

	timelyPublishedAt := windowStart.Add(2 * time.Minute)
	timelyDetectedAt := timelyPublishedAt.Add(30 * time.Second)
	latePublishedAt := windowStart.Add(5 * time.Minute)
	lateDetectedAt := windowEnd.Add(time.Minute)
	require.NoError(t, db.Create([]sqliteTelemetryTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         timelyOutbox.ContentID,
			ChannelID:         timelyOutbox.ChannelID,
			ActualPublishedAt: &timelyPublishedAt,
			DetectedAt:        timelyDetectedAt,
			CreatedAt:         timelyDetectedAt,
			UpdatedAt:         timelyDetectedAt,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         lateOutbox.ContentID,
			ChannelID:         lateOutbox.ChannelID,
			ActualPublishedAt: &latePublishedAt,
			DetectedAt:        lateDetectedAt,
			CreatedAt:         lateDetectedAt,
			UpdatedAt:         lateDetectedAt,
		},
	}).Error)

	require.NoError(t, db.Create([]sqliteTelemetryBufferModel{
		{
			DeliveryID:     9101,
			AttemptOrdinal: 1,
			OutboxID:       timelyOutbox.ID,
			ChannelID:      timelyOutbox.ChannelID,
			ContentID:      timelyOutbox.ContentID,
			PostID:         timelyOutbox.ContentID,
			RoomID:         "room-timely",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-timely",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        timelyDetectedAt.Add(time.Minute),
			NextAttemptAt:  timelyDetectedAt.Add(time.Minute),
		},
		{
			DeliveryID:     9102,
			AttemptOrdinal: 1,
			OutboxID:       lateOutbox.ID,
			ChannelID:      lateOutbox.ChannelID,
			ContentID:      lateOutbox.ContentID,
			PostID:         lateOutbox.ContentID,
			RoomID:         "room-late",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-late",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        lateDetectedAt.Add(time.Minute),
			NextAttemptAt:  lateDetectedAt.Add(time.Minute),
		},
	}).Error)

	repo := NewDeliveryTelemetryRepository(db)
	rows, err := repo.ListPostSendCountsWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, timelyOutbox.ContentID, rows[0].ContentID)
}

func TestDeliveryTelemetryRepository_ListPostSendCountsByFinalizedObservationWindow_UsesFrozenBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteTelemetryOutboxModel{},
		&sqliteTelemetryBufferModel{},
		&sqliteTelemetryTrackingModel{},
		&sqliteTelemetryObservationBaselineModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	timelyPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	timelyDetectedAt := timelyPublishedAt.Add(20 * time.Second)
	zeroDetectedAt := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
	latePublishedAt := time.Date(2026, 4, 11, 2, 0, 0, 0, time.UTC)
	lateDetectedAt := latePublishedAt.Add(30 * time.Second)

	timelyOutbox := sqliteTelemetryOutboxModel{
		ID:            9201,
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_COMMUNITY",
		ContentID:     "community:post-timely",
		Payload:       `{"post_id":"community:post-timely"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: timelyDetectedAt,
		CreatedAt:     timelyDetectedAt,
	}
	lateOutbox := sqliteTelemetryOutboxModel{
		ID:            9202,
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_LATE",
		ContentID:     "short:late-after-freeze",
		Payload:       `{"post_id":"short:late-after-freeze"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: lateDetectedAt,
		CreatedAt:     lateDetectedAt,
	}
	require.NoError(t, db.Create([]sqliteTelemetryOutboxModel{timelyOutbox, lateOutbox}).Error)

	require.NoError(t, db.Create([]sqliteTelemetryTrackingModel{
		{
			Kind:               string(domain.OutboxKindCommunityPost),
			ContentID:          timelyOutbox.ContentID,
			CanonicalContentID: timelyOutbox.ContentID,
			ChannelID:          timelyOutbox.ChannelID,
			ActualPublishedAt:  &timelyPublishedAt,
			DetectedAt:         timelyDetectedAt,
			CreatedAt:          timelyDetectedAt,
			UpdatedAt:          timelyDetectedAt,
		},
		{
			Kind:               string(domain.OutboxKindNewShort),
			ContentID:          lateOutbox.ContentID,
			CanonicalContentID: lateOutbox.ContentID,
			ChannelID:          lateOutbox.ChannelID,
			ActualPublishedAt:  &latePublishedAt,
			DetectedAt:         lateDetectedAt,
			CreatedAt:          lateDetectedAt,
			UpdatedAt:          lateDetectedAt,
		},
	}).Error)
	finalizedRows := []sqliteTelemetryObservationBaselineModel{
		{
			RuntimeName:       "youtube-scraper",
			BigBangCutoverAt:  cutoverAt,
			Kind:              string(domain.OutboxKindCommunityPost),
			PostID:            timelyOutbox.ContentID,
			ChannelID:         timelyOutbox.ChannelID,
			ActualPublishedAt: &timelyPublishedAt,
			DetectedAt:        timelyDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:      "youtube-scraper",
			BigBangCutoverAt: cutoverAt,
			Kind:             string(domain.OutboxKindNewShort),
			PostID:           "short:zero-send",
			ChannelID:        "UC_ZERO",
			DetectedAt:       zeroDetectedAt,
			FinalizedAt:      finalizedAt,
		},
	}
	require.NoError(t, db.Create(&finalizedRows).Error)

	require.NoError(t, db.Create([]sqliteTelemetryBufferModel{
		{
			DeliveryID:     9201,
			AttemptOrdinal: 1,
			OutboxID:       timelyOutbox.ID,
			ChannelID:      timelyOutbox.ChannelID,
			ContentID:      timelyOutbox.ContentID,
			PostID:         timelyOutbox.ContentID,
			RoomID:         "room-timely",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:community:post-timely",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        timelyDetectedAt.Add(time.Minute),
			NextAttemptAt:  timelyDetectedAt.Add(time.Minute),
		},
		{
			DeliveryID:     9202,
			AttemptOrdinal: 1,
			OutboxID:       lateOutbox.ID,
			ChannelID:      lateOutbox.ChannelID,
			ContentID:      lateOutbox.ContentID,
			PostID:         lateOutbox.ContentID,
			RoomID:         "room-late",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short:late-after-freeze",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        lateDetectedAt.Add(time.Minute),
			NextAttemptAt:  lateDetectedAt.Add(time.Minute),
		},
	}).Error)

	repo := NewDeliveryTelemetryRepository(db)
	rows, err := repo.ListPostSendCountsByFinalizedObservationWindow(ctx, "youtube-scraper", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byPostID := make(map[string]PostSendCount, len(rows))
	for i := range rows {
		byPostID[rows[i].PostID] = rows[i]
	}

	timelyRow, ok := byPostID["community:post-timely"]
	require.True(t, ok)
	require.Equal(t, int64(1), timelyRow.OutboxCount)
	require.Equal(t, int64(1), timelyRow.SuccessSendCount)
	require.Equal(t, timelyOutbox.ChannelID, timelyRow.ChannelID)

	zeroRow, ok := byPostID["short:zero-send"]
	require.True(t, ok)
	require.Equal(t, int64(0), zeroRow.OutboxCount)
	require.Equal(t, int64(0), zeroRow.SuccessSendCount)
	require.Equal(t, "short:zero-send", zeroRow.ContentID)
	require.Nil(t, zeroRow.ActualPublishedAt)
	require.NotNil(t, zeroRow.DetectedAt)
	require.Equal(t, zeroDetectedAt, zeroRow.DetectedAt.UTC())

	_, exists := byPostID["short:late-after-freeze"]
	require.False(t, exists)
}
