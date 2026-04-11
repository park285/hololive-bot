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

type observationTestBufferModel struct {
	ID                          int64  `gorm:"primaryKey;autoIncrement"`
	DeliveryID                  int64  `gorm:"not null;uniqueIndex:idx_obs_delivery_attempt"`
	AttemptOrdinal              int    `gorm:"not null;uniqueIndex:idx_obs_delivery_attempt"`
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

func (observationTestBufferModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}

type observationTestTrackingModel struct {
	Kind                        domain.OutboxKind `gorm:"primaryKey"`
	ContentID                   string            `gorm:"primaryKey"`
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

func (observationTestTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

type observationTestWindowModel struct {
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

func (observationTestWindowModel) TableName() string {
	return "youtube_community_shorts_observation_windows"
}

func TestDeliveryTelemetryRepository_EnqueueEnrichesObservationWindowContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&observationTestBufferModel{}, &observationTestTrackingModel{}, &observationTestWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	actualPublishedAt := observationStartedAt.Add(42 * time.Minute)
	detectedAt := actualPublishedAt.Add(15 * time.Second)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-inside", "UC_inside", &actualPublishedAt, detectedAt)

	repo := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     101,
		AttemptOrdinal: 1,
		OutboxID:       201,
		ChannelID:      "UC_inside",
		ContentID:      "short-inside",
		RoomID:         "room-inside",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-inside",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        detectedAt.Add(30 * time.Second),
	}}))

	var saved observationTestBufferModel
	require.NoError(t, db.First(&saved).Error)
	require.NotNil(t, saved.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, saved.ActualPublishedAt.UTC())
	require.NotNil(t, saved.DetectedAt)
	require.Equal(t, detectedAt, saved.DetectedAt.UTC())
	require.NotNil(t, saved.AlarmSentAt)
	require.Equal(t, actualPublishedAt.Add(90*time.Second), saved.AlarmSentAt.UTC())
	require.NotNil(t, saved.AlarmLatencyMillis)
	require.Equal(t, int64(90*time.Second/time.Millisecond), *saved.AlarmLatencyMillis)
	require.Equal(t, deliveryTelemetryObservationStatusMatched, saved.ObservationStatus)
	require.Equal(t, "youtube-scraper", saved.ObservationRuntimeName)
	require.NotNil(t, saved.ObservationBigBangCutoverAt)
	require.Equal(t, cutoverAt, saved.ObservationBigBangCutoverAt.UTC())
	require.NotNil(t, saved.ObservationStartedAt)
	require.Equal(t, observationStartedAt, saved.ObservationStartedAt.UTC())
	require.NotNil(t, saved.ObservationEndedAt)
	require.Equal(t, observationStartedAt.Add(24*time.Hour), saved.ObservationEndedAt.UTC())
}

func TestDeliveryTelemetryRepository_EnqueueMarksLateDetectionsOutsideObservationWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&observationTestBufferModel{}, &observationTestTrackingModel{}, &observationTestWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	actualPublishedAt := observationStartedAt.Add(42 * time.Minute)
	detectedAt := observationStartedAt.Add(24*time.Hour + time.Minute)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-late-detect", "UC_late", &actualPublishedAt, detectedAt)

	repo := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     111,
		AttemptOrdinal: 1,
		OutboxID:       211,
		ChannelID:      "UC_late",
		ContentID:      "short-late-detect",
		RoomID:         "room-late",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-late-detect",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        detectedAt.Add(30 * time.Second),
	}}))

	var saved observationTestBufferModel
	require.NoError(t, db.First(&saved).Error)
	require.Equal(t, deliveryTelemetryObservationStatusOutsideWindow, saved.ObservationStatus)
	require.Equal(t, "", saved.ObservationRuntimeName)
	require.Nil(t, saved.ObservationBigBangCutoverAt)
	require.Nil(t, saved.ObservationStartedAt)
	require.Nil(t, saved.ObservationEndedAt)
}

func TestDeliveryTelemetryRepository_ListByObservationWindowReturnsMatchedOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&observationTestBufferModel{}, &observationTestTrackingModel{}, &observationTestWindowModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)

	insidePublishedAt := observationStartedAt.Add(5 * time.Minute)
	insideDetectedAt := insidePublishedAt.Add(20 * time.Second)
	seedTrackingRow(t, db, domain.OutboxKindCommunityPost, "post-inside", "UC_inside", &insidePublishedAt, insideDetectedAt)

	outsidePublishedAt := observationStartedAt.Add(25 * time.Hour)
	outsideDetectedAt := outsidePublishedAt.Add(20 * time.Second)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-outside", "UC_outside", &outsidePublishedAt, outsideDetectedAt)

	repo := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{
		{
			DeliveryID:     201,
			AttemptOrdinal: 1,
			OutboxID:       301,
			ChannelID:      "UC_inside",
			ContentID:      "post-inside",
			RoomID:         "room-1",
			AlarmType:      domain.AlarmTypeCommunity,
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-inside",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        insideDetectedAt.Add(time.Minute),
		},
		{
			DeliveryID:     202,
			AttemptOrdinal: 1,
			OutboxID:       302,
			ChannelID:      "UC_outside",
			ContentID:      "short-outside",
			RoomID:         "room-2",
			AlarmType:      domain.AlarmTypeShorts,
			DedupeKey:      "youtube-notification:NEW_SHORT:short-outside",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        outsideDetectedAt.Add(time.Minute),
		},
	}))

	rows, err := repo.ListByObservationWindow(ctx, "youtube-scraper", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(201), rows[0].DeliveryID)
	require.Equal(t, "post-inside", rows[0].ContentID)
	require.Equal(t, deliveryTelemetryObservationStatusMatched, rows[0].ObservationStatus)
}

func TestDeliveryTelemetryRepository_ListByFinalizedObservationWindowUsesFrozenBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteTelemetryOutboxModel{},
		&observationTestBufferModel{},
		&observationTestTrackingModel{},
		&observationTestWindowModel{},
		&sqliteTelemetryObservationBaselineModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	finalizedAt := observationStartedAt.Add(24 * time.Hour)
	require.NoError(t, db.Create(&observationTestWindowModel{
		RuntimeName:             "youtube-scraper",
		BigBangCutoverAt:        cutoverAt,
		AppVersion:              "v-test",
		TargetChannelCount:      1,
		DeploymentCompletedAt:   observationStartedAt,
		ObservationStartedAt:    observationStartedAt,
		ObservationEndedAt:      finalizedAt,
		ClosedAt:                &finalizedAt,
		FinalizedPostBaselineAt: &finalizedAt,
		FinalizedPostCount:      1,
	}).Error)

	insidePublishedAt := observationStartedAt.Add(5 * time.Minute)
	insideDetectedAt := insidePublishedAt.Add(20 * time.Second)
	latePublishedAt := observationStartedAt.Add(25 * time.Hour)
	lateDetectedAt := latePublishedAt.Add(20 * time.Second)
	require.NoError(t, db.Create([]observationTestTrackingModel{
		{
			Kind:               domain.OutboxKindCommunityPost,
			ContentID:          "post-inside",
			CanonicalContentID: "post-inside",
			ChannelID:          "UC_inside",
			ActualPublishedAt:  &insidePublishedAt,
			DetectedAt:         insideDetectedAt,
		},
		{
			Kind:               domain.OutboxKindNewShort,
			ContentID:          "short-late",
			CanonicalContentID: "short-late",
			ChannelID:          "UC_late",
			ActualPublishedAt:  &latePublishedAt,
			DetectedAt:         lateDetectedAt,
		},
	}).Error)
	require.NoError(t, db.Create([]sqliteTelemetryOutboxModel{
		{
			ID:            401,
			Kind:          string(domain.OutboxKindCommunityPost),
			ChannelID:     "UC_inside",
			ContentID:     "post-inside",
			Payload:       `{"post_id":"post-inside"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: insideDetectedAt,
			CreatedAt:     insideDetectedAt,
		},
		{
			ID:            402,
			Kind:          string(domain.OutboxKindNewShort),
			ChannelID:     "UC_late",
			ContentID:     "short-late",
			Payload:       `{"post_id":"short-late"}`,
			Status:        string(domain.OutboxStatusSent),
			AttemptCount:  0,
			NextAttemptAt: lateDetectedAt,
			CreatedAt:     lateDetectedAt,
		},
	}).Error)
	require.NoError(t, db.Create([]sqliteTelemetryObservationBaselineModel{{
		RuntimeName:       "youtube-scraper",
		BigBangCutoverAt:  cutoverAt,
		Kind:              string(domain.OutboxKindCommunityPost),
		PostID:            "post-inside",
		ChannelID:         "UC_inside",
		ActualPublishedAt: &insidePublishedAt,
		DetectedAt:        insideDetectedAt,
		FinalizedAt:       finalizedAt,
	}}).Error)
	require.NoError(t, db.Create([]observationTestBufferModel{
		{
			DeliveryID:     301,
			AttemptOrdinal: 1,
			OutboxID:       401,
			ChannelID:      "UC_inside",
			ContentID:      "post-inside",
			PostID:         "post-inside",
			RoomID:         "room-1",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-inside",
			DeliveryPath:   communityShortsDeliveryPath,
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        insideDetectedAt.Add(time.Minute),
			NextAttemptAt:  insideDetectedAt.Add(time.Minute),
		},
		{
			DeliveryID:     302,
			AttemptOrdinal: 1,
			OutboxID:       402,
			ChannelID:      "UC_late",
			ContentID:      "short-late",
			PostID:         "short-late",
			RoomID:         "room-2",
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
	rows, err := repo.ListByFinalizedObservationWindow(ctx, "youtube-scraper", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(301), rows[0].DeliveryID)
	require.Equal(t, "post-inside", rows[0].ContentID)
}

func seedObservationWindow(t *testing.T, db *gorm.DB, cutoverAt, observationStartedAt time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&observationTestWindowModel{
		RuntimeName:           "youtube-scraper",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "v-test",
		TargetChannelCount:    1,
		DeploymentCompletedAt: observationStartedAt,
		ObservationStartedAt:  observationStartedAt,
		ObservationEndedAt:    observationStartedAt.Add(24 * time.Hour),
	}).Error)
}

func seedTrackingRow(
	t *testing.T,
	db *gorm.DB,
	kind domain.OutboxKind,
	contentID string,
	channelID string,
	actualPublishedAt *time.Time,
	detectedAt time.Time,
) {
	t.Helper()
	record := observationTestTrackingModel{
		Kind:              kind,
		ContentID:         contentID,
		ChannelID:         channelID,
		ActualPublishedAt: actualPublishedAt,
		DetectedAt:        detectedAt,
	}
	if actualPublishedAt != nil {
		alarmSentAt := actualPublishedAt.Add(90 * time.Second)
		alarmLatencyMillis := int64(alarmSentAt.Sub(actualPublishedAt.UTC()) / time.Millisecond)
		record.AlarmSentAt = &alarmSentAt
		record.AlarmLatencyMillis = &alarmLatencyMillis
	}
	require.NoError(t, db.Create(&record).Error)
}
