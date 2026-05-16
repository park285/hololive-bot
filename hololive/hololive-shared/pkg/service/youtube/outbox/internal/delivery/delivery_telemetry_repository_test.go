package delivery

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type sqliteTelemetryOutboxModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	Kind          string    `gorm:"type:text;not null"`
	ChannelID     string    `gorm:"type:text;not null"`
	ContentID     string    `gorm:"type:text;not null"`
	Payload       string    `gorm:"type:text;not null"`
	Status        string    `gorm:"type:text;not null"`
	AttemptCount  int       `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `gorm:"type:text"`
}

func (sqliteTelemetryOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

type sqliteTelemetryDeliveryModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	OutboxID      int64     `gorm:"not null;index:idx_ynd_outbox_room,unique"`
	RoomID        string    `gorm:"type:text;not null;index:idx_ynd_outbox_room,unique"`
	Status        string    `gorm:"type:text;not null"`
	AttemptCount  int       `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `gorm:"type:text"`
}

func (sqliteTelemetryDeliveryModel) TableName() string {
	return "youtube_notification_delivery"
}

type sqliteTelemetryBufferModel struct {
	ID                          int64  `gorm:"primaryKey;autoIncrement"`
	DeliveryID                  int64  `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt"`
	AttemptOrdinal              int    `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt"`
	OutboxID                    int64  `gorm:"not null"`
	ChannelID                   string `gorm:"type:text;not null"`
	ContentID                   string `gorm:"type:text;not null"`
	PostID                      string `gorm:"type:text;not null"`
	RoomID                      string `gorm:"type:text;not null"`
	AlarmType                   string `gorm:"type:text;not null"`
	ActualPublishedAt           *time.Time
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	DetectedAt                  *time.Time
	ObservationStatus           string     `gorm:"type:text;not null"`
	ObservationRuntimeName      string     `gorm:"type:text"`
	ObservationBigBangCutoverAt *time.Time `gorm:"column:observation_bigbang_cutover_at"`
	ObservationStartedAt        *time.Time
	ObservationEndedAt          *time.Time
	DedupeKey                   string `gorm:"type:text;not null"`
	DeliveryPath                string `gorm:"type:text;not null"`
	DeliveryMode                string `gorm:"type:text;not null"`
	SendResult                  string `gorm:"type:text;not null"`
	FailureReason               string `gorm:"type:text"`
	AttemptStartedAt            *time.Time
	AttemptFinishedAt           *time.Time
	EventAt                     time.Time `gorm:"not null"`
	NextAttemptAt               time.Time `gorm:"not null"`
	CreatedAt                   time.Time
	LockedAt                    *time.Time
	LoggedAt                    *time.Time
	Error                       string `gorm:"type:text"`
}

func (sqliteTelemetryBufferModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}

type sqliteTelemetryObservationTrackingModel struct {
	Kind                        string `gorm:"primaryKey;type:text"`
	ContentID                   string `gorm:"primaryKey;type:text"`
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

func (sqliteTelemetryObservationTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

type sqliteTelemetryObservationWindowModel struct {
	RuntimeName             string     `gorm:"primaryKey;type:text"`
	BigBangCutoverAt        time.Time  `gorm:"primaryKey;column:bigbang_cutover_at"`
	AppVersion              string     `gorm:"type:text;not null"`
	TargetChannelCount      int        `gorm:"not null"`
	DeploymentCompletedAt   time.Time  `gorm:"not null"`
	ObservationStartedAt    time.Time  `gorm:"not null"`
	ObservationEndedAt      time.Time  `gorm:"not null"`
	ClosedAt                *time.Time `gorm:"column:closed_at"`
	FinalizedPostBaselineAt *time.Time `gorm:"column:finalized_post_baseline_at"`
	FinalizedPostCount      int        `gorm:"not null;default:0"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (sqliteTelemetryObservationWindowModel) TableName() string {
	return "youtube_community_shorts_observation_windows"
}

type sqliteTelemetryObservationBaselineModel struct {
	RuntimeName       string    `gorm:"primaryKey;type:text"`
	BigBangCutoverAt  time.Time `gorm:"primaryKey;column:bigbang_cutover_at"`
	Kind              string    `gorm:"primaryKey;type:text"`
	PostID            string    `gorm:"primaryKey;type:text"`
	ChannelID         string    `gorm:"type:text;not null"`
	ActualPublishedAt *time.Time
	DetectedAt        time.Time `gorm:"not null"`
	FinalizedAt       time.Time `gorm:"not null"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (sqliteTelemetryObservationBaselineModel) TableName() string {
	return "youtube_community_shorts_observation_post_baselines"
}

func TestDeliveryTelemetryRepository_BackfillAndFlush(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	sentAt := time.Now().UTC().Add(-30 * time.Second)
	outbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_backfill",
		ContentID:     "post-backfill",
		Payload:       `{"post_id":"post-backfill","content_text":"hello"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&outbox).Error)

	delivery := sqliteTelemetryDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-backfill",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	actualPublishedAt := sentAt.Add(-2 * time.Minute)
	detectedAt := sentAt.Add(-1 * time.Minute)
	alarmLatencyMillis := int64(sentAt.Sub(actualPublishedAt) / time.Millisecond)
	alarmLatencyExceeded := false
	require.NoError(t, db.Create(&sqliteTelemetryObservationTrackingModel{
		Kind:                 string(domain.OutboxKindCommunityPost),
		ContentID:            outbox.ContentID,
		ChannelID:            outbox.ChannelID,
		ActualPublishedAt:    &actualPublishedAt,
		DetectedAt:           detectedAt,
		AlarmSentAt:          &sentAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: &alarmLatencyExceeded,
	}).Error)

	repo := NewDeliveryTelemetryRepository(db)

	inserted, err := repo.BackfillFromDelivery(ctx, 10, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	pending, err := repo.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, delivery.ID, pending[0].DeliveryID)
	require.Equal(t, 1, pending[0].AttemptOrdinal)
	require.Equal(t, "success", pending[0].SendResult)
	require.Equal(t, string(domain.AlarmTypeCommunity), string(pending[0].AlarmType))
	require.Equal(t, communityShortsDeliveryPath, pending[0].DeliveryPath)
	require.Equal(t, "post-backfill", pending[0].PostID)
	require.Nil(t, pending[0].AttemptStartedAt)
	require.NotNil(t, pending[0].AttemptFinishedAt)
	require.Equal(t, sentAt, pending[0].AttemptFinishedAt.UTC())
	require.NotNil(t, pending[0].AlarmSentAt)
	require.Equal(t, sentAt, pending[0].AlarmSentAt.UTC())
	require.NotNil(t, pending[0].AlarmLatencyMillis)
	require.Equal(t, alarmLatencyMillis, *pending[0].AlarmLatencyMillis)

	require.NoError(t, repo.MarkLoggedBatch(ctx, []int64{pending[0].ID}))

	var saved sqliteTelemetryBufferModel
	require.NoError(t, db.First(&saved, pending[0].ID).Error)
	require.NotNil(t, saved.LoggedAt)
	require.Equal(t, "post-backfill", saved.PostID)
}

func TestDeliveryTelemetryRepository_EnqueueDedupesByDeliveryAttempt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	repo := NewDeliveryTelemetryRepository(db)
	event := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     101,
		AttemptOrdinal: 1,
		OutboxID:       201,
		ChannelID:      "UC_dedupe",
		ContentID:      "short-1",
		RoomID:         "room-1",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-1",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Now().UTC(),
	}

	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{event}))
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{event}))

	var count int64
	require.NoError(t, db.Model(&sqliteTelemetryBufferModel{}).Count(&count).Error)
	require.Equal(t, int64(1), count)

	var saved sqliteTelemetryBufferModel
	require.NoError(t, db.First(&saved).Error)
	require.Equal(t, "short-1", saved.PostID)
}

func TestDeliveryTelemetryRepository_BackfillFromDelivery_AppliesRetentionCutoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	oldSentAt := now.Add(-25 * time.Hour)
	recentSentAt := now.Add(-2 * time.Hour)

	oldOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_old",
		ContentID:     "post-old",
		Payload:       `{"post_id":"post-old","content_text":"old"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: oldSentAt,
		SentAt:        &oldSentAt,
	}
	require.NoError(t, db.Create(&oldOutbox).Error)
	require.NoError(t, db.Create(&sqliteTelemetryDeliveryModel{
		OutboxID:      oldOutbox.ID,
		RoomID:        "room-old",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: oldSentAt,
		CreatedAt:     oldSentAt,
		SentAt:        &oldSentAt,
	}).Error)

	recentOutbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_recent",
		ContentID:     "short-recent",
		Payload:       `{"video_id":"short-recent","title":"recent"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: recentSentAt,
		SentAt:        &recentSentAt,
	}
	require.NoError(t, db.Create(&recentOutbox).Error)
	require.NoError(t, db.Create(&sqliteTelemetryDeliveryModel{
		OutboxID:      recentOutbox.ID,
		RoomID:        "room-recent",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: recentSentAt,
		CreatedAt:     recentSentAt,
		SentAt:        &recentSentAt,
	}).Error)

	repo := NewDeliveryTelemetryRepository(db)
	inserted, err := repo.BackfillFromDelivery(ctx, 10, now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	var rows []sqliteTelemetryBufferModel
	require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "short-recent", rows[0].ContentID)
	require.Equal(t, "short-recent", rows[0].PostID)
}

func TestDispatcher_Cleanup_RemovesOnlyLoggedTelemetryOlderThanRetention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	oldLoggedAt := now.Add(-25 * time.Hour)
	recentLoggedAt := now.Add(-2 * time.Hour)

	require.NoError(t, db.Create(&sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_cleanup",
		ContentID:     "cleanup-outbox",
		Payload:       `{"post_id":"cleanup-outbox","content_text":"cleanup"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now.Add(-48 * time.Hour),
		SentAt:        &oldLoggedAt,
	}).Error)

	rows := []sqliteTelemetryBufferModel{
		{
			DeliveryID:     701,
			AttemptOrdinal: 1,
			OutboxID:       1,
			ChannelID:      "UC_cleanup",
			ContentID:      "old-logged",
			PostID:         "old-logged",
			RoomID:         "room-old",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:old-logged",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        oldLoggedAt,
			NextAttemptAt:  oldLoggedAt,
			LoggedAt:       &oldLoggedAt,
		},
		{
			DeliveryID:     702,
			AttemptOrdinal: 1,
			OutboxID:       1,
			ChannelID:      "UC_cleanup",
			ContentID:      "recent-logged",
			PostID:         "recent-logged",
			RoomID:         "room-recent",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:recent-logged",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        recentLoggedAt,
			NextAttemptAt:  recentLoggedAt,
			LoggedAt:       &recentLoggedAt,
		},
		{
			DeliveryID:     703,
			AttemptOrdinal: 1,
			OutboxID:       1,
			ChannelID:      "UC_cleanup",
			ContentID:      "old-pending",
			PostID:         "old-pending",
			RoomID:         "room-pending",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:old-pending",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "failure",
			EventAt:        oldLoggedAt,
			NextAttemptAt:  now,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		CleanupAfter:       7 * 24 * time.Hour,
		CleanupEnabled:     false,
		TelemetryRetention: 24 * time.Hour,
	})
	dispatcher.CleanupForTest(ctx)

	var remaining []sqliteTelemetryBufferModel
	require.NoError(t, db.Order("content_id ASC").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.Equal(t, "old-pending", remaining[0].ContentID)
	require.Equal(t, "recent-logged", remaining[1].ContentID)
}

func TestDispatcher_ProcessDeliveryTelemetry_EmitsBufferedAuditLogs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	sentAt := time.Now().UTC().Add(-time.Minute)
	outbox := sqliteTelemetryOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_emit",
		ContentID:     "short-emit",
		Payload:       `{"video_id":"short-emit","title":"emit"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&outbox).Error)

	delivery := sqliteTelemetryDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-emit",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := sentAt.Add(-15 * time.Minute).UTC()
	actualPublishedAt := observationStartedAt.Add(2 * time.Minute)
	detectedAt := actualPublishedAt.Add(20 * time.Second)
	alarmSentAt := actualPublishedAt.Add(3 * time.Minute)
	alarmLatencyMillis := int64(alarmSentAt.Sub(actualPublishedAt) / time.Millisecond)
	alarmLatencyExceeded := true
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)
	require.NoError(t, db.Create(&sqliteTelemetryObservationTrackingModel{
		Kind:                 string(domain.OutboxKindNewShort),
		ContentID:            "short-emit",
		ChannelID:            "UC_emit",
		ActualPublishedAt:    &actualPublishedAt,
		DetectedAt:           detectedAt,
		AlarmSentAt:          &alarmSentAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: &alarmLatencyExceeded,
	}).Error)

	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, logger, Config{
		LockTimeout:            time.Minute,
		TelemetryBackfillBatch: 10,
		TelemetryFlushBatch:    10,
	})

	dispatcher.processDeliveryTelemetry(ctx)

	var rows []sqliteTelemetryBufferModel
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].LoggedAt)
	require.Equal(t, "short-emit", rows[0].PostID)
	require.Contains(t, logBuffer.String(), deliveryAuditLogMessage)
	require.Contains(t, logBuffer.String(), "\"delivery_path\":\""+communityShortsDeliveryPath+"\"")
	require.Contains(t, logBuffer.String(), "\"post_id\":\"short-emit\"")
	require.Contains(t, logBuffer.String(), "\"observation_status\":\"matched\"")
	require.Contains(t, logBuffer.String(), "\"observation_runtime_name\":\"youtube-scraper\"")
	require.Contains(t, logBuffer.String(), "\"actual_published_at\":")
	require.Contains(t, logBuffer.String(), "\"alarm_latency_exceeded\":true")
	require.Contains(t, logBuffer.String(), "\"latency_classification\":{")
	require.Contains(t, logBuffer.String(), "\"delay_source\":")
}

func TestDeliveryTelemetryRepository_MarkRetryReleasesLock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}, &sqliteTelemetryObservationWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	repo := NewDeliveryTelemetryRepository(db)
	now := time.Now().UTC()
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     501,
		AttemptOrdinal: 1,
		OutboxID:       601,
		ChannelID:      "UC_retry",
		ContentID:      "post-retry",
		RoomID:         "room-retry",
		AlarmType:      domain.AlarmTypeCommunity,
		DedupeKey:      "youtube-notification:COMMUNITY_POST:post-retry",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "grouped",
		SendResult:     "failure",
		FailureReason:  "send message",
		EventAt:        now,
		NextAttemptAt:  now,
	}}))

	locked, err := repo.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, locked, 1)
	require.NoError(t, repo.MarkRetryBatch(ctx, []int64{locked[0].ID}, time.Millisecond, "emit failed"))

	time.Sleep(2 * time.Millisecond)
	again, err := repo.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, locked[0].ID, again[0].ID)
	require.Equal(t, "post-retry", again[0].PostID)
	require.NoError(t, repo.MarkLoggedBatch(ctx, []int64{again[0].ID}))
}

var _ = io.Discard
