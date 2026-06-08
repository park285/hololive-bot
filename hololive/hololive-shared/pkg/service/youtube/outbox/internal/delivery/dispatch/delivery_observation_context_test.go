package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type observationTestBufferModel struct {
	ID                          int64  `db:"id"`
	DeliveryID                  int64  `db:"delivery_id"`
	AttemptOrdinal              int    `db:"attempt_ordinal"`
	OutboxID                    int64  `db:"outbox_id"`
	ChannelID                   string `db:"channel_id"`
	ContentID                   string `db:"content_id"`
	PostID                      string `db:"post_id"`
	RoomID                      string `db:"room_id"`
	AlarmType                   string `db:"alarm_type"`
	ActualPublishedAt           *time.Time
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	DetectedAt                  *time.Time
	ObservationStatus           string     `db:"observation_status"`
	ObservationRuntimeName      string     `db:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `db:"observation_bigbang_cutover_at"`
	ObservationStartedAt        *time.Time
	ObservationEndedAt          *time.Time
	DedupeKey                   string `db:"dedupe_key"`
	DeliveryPath                string `db:"delivery_path"`
	DeliveryMode                string `db:"delivery_mode"`
	SendResult                  string `db:"send_result"`
	FailureReason               string `db:"failure_reason"`
	AttemptStartedAt            *time.Time
	AttemptFinishedAt           *time.Time
	EventAt                     time.Time `db:"event_at"`
	NextAttemptAt               time.Time `db:"next_attempt_at"`
	CreatedAt                   time.Time
	LockedAt                    *time.Time
	LoggedAt                    *time.Time
	Error                       string `db:"error"`
}

func (observationTestBufferModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}

type observationTestTrackingModel struct {
	Kind                        domain.OutboxKind `db:"kind"`
	ContentID                   string            `db:"content_id"`
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

func (observationTestTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

type observationTestWindowModel struct {
	RuntimeName             string     `db:"runtime_name"`
	BigBangCutoverAt        time.Time  `db:"bigbang_cutover_at"`
	AppVersion              string     `db:"app_version"`
	TargetChannelCount      int        `db:"target_channel_count"`
	DeploymentCompletedAt   time.Time  `db:"deployment_completed_at"`
	ObservationStartedAt    time.Time  `db:"observation_started_at"`
	ObservationEndedAt      time.Time  `db:"observation_ended_at"`
	ClosedAt                *time.Time `db:"closed_at"`
	FinalizedPostBaselineAt *time.Time `db:"finalized_post_baseline_at"`
	FinalizedPostCount      int        `db:"finalized_post_count"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (observationTestWindowModel) TableName() string {
	return "youtube_community_shorts_observation_windows"
}

func TestDeliveryTelemetryRepository_EnqueueEnrichesObservationWindowContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	actualPublishedAt := observationStartedAt.Add(42 * time.Minute)
	detectedAt := actualPublishedAt.Add(15 * time.Second)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-inside", "UC_inside", &actualPublishedAt, detectedAt)

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
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
	require.NoError(t, firstDeliveryTestRow(db, &saved).Error)
	require.NotNil(t, saved.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, saved.ActualPublishedAt.UTC())
	require.NotNil(t, saved.DetectedAt)
	require.Equal(t, detectedAt, saved.DetectedAt.UTC())
	require.NotNil(t, saved.AlarmSentAt)
	require.Equal(t, actualPublishedAt.Add(90*time.Second), saved.AlarmSentAt.UTC())
	require.NotNil(t, saved.AlarmLatencyMillis)
	require.Equal(t, int64(90*time.Second/time.Millisecond), *saved.AlarmLatencyMillis)
	require.Equal(t, deliveryTelemetryObservationStatusMatched, saved.ObservationStatus)
	require.Equal(t, "youtube-producer", saved.ObservationRuntimeName)
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
	db := newDeliveryPool(t)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	actualPublishedAt := observationStartedAt.Add(42 * time.Minute)
	detectedAt := observationStartedAt.Add(24*time.Hour + time.Minute)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-late-detect", "UC_late", &actualPublishedAt, detectedAt)

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
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
	require.NoError(t, firstDeliveryTestRow(db, &saved).Error)
	require.Equal(t, deliveryTelemetryObservationStatusOutsideWindow, saved.ObservationStatus)
	require.Equal(t, "", saved.ObservationRuntimeName)
	require.Nil(t, saved.ObservationBigBangCutoverAt)
	require.Nil(t, saved.ObservationStartedAt)
	require.Nil(t, saved.ObservationEndedAt)
}

func TestDeliveryTelemetryRepository_ListByObservationWindowReturnsMatchedOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	seedObservationWindow(t, db, cutoverAt, observationStartedAt)

	insidePublishedAt := observationStartedAt.Add(5 * time.Minute)
	insideDetectedAt := insidePublishedAt.Add(20 * time.Second)
	seedTrackingRow(t, db, domain.OutboxKindCommunityPost, "post-inside", "UC_inside", &insidePublishedAt, insideDetectedAt)

	outsidePublishedAt := observationStartedAt.Add(25 * time.Hour)
	outsideDetectedAt := outsidePublishedAt.Add(20 * time.Second)
	seedTrackingRow(t, db, domain.OutboxKindNewShort, "short-outside", "UC_outside", &outsidePublishedAt, outsideDetectedAt)

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{
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

	rows, err := repository.ListByObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(201), rows[0].DeliveryID)
	require.Equal(t, "post-inside", rows[0].ContentID)
	require.Equal(t, deliveryTelemetryObservationStatusMatched, rows[0].ObservationStatus)
}

func TestDeliveryTelemetryRepository_ListByFinalizedObservationWindowUsesFrozenBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	finalizedAt := observationStartedAt.Add(24 * time.Hour)
	require.NoError(t, insertDeliveryTestRows(db, &observationTestWindowModel{
		RuntimeName:             "youtube-producer",
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
	require.NoError(t, insertDeliveryTestRows(db, []observationTestTrackingModel{
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
	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestOutboxModel{
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
	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestObservationBaselineModel{{
		RuntimeName:       "youtube-producer",
		BigBangCutoverAt:  cutoverAt,
		Kind:              string(domain.OutboxKindCommunityPost),
		PostID:            "post-inside",
		ChannelID:         "UC_inside",
		ActualPublishedAt: &insidePublishedAt,
		DetectedAt:        insideDetectedAt,
		FinalizedAt:       finalizedAt,
	}}).Error)
	require.NoError(t, insertDeliveryTestRows(db, []observationTestBufferModel{
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

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListByFinalizedObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(301), rows[0].DeliveryID)
	require.Equal(t, "post-inside", rows[0].ContentID)
}

func seedObservationWindow(t *testing.T, db *deliveryTestDB, cutoverAt, observationStartedAt time.Time) {
	t.Helper()
	require.NoError(t, insertDeliveryTestRows(db, &observationTestWindowModel{
		RuntimeName:           "youtube-producer",
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
	db *deliveryTestDB,
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
	require.NoError(t, insertDeliveryTestRows(db, &record).Error)
}
