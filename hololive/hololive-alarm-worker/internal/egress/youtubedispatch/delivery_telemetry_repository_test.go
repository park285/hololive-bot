package youtubedispatch

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type deliveryTelemetryTestOutboxModel struct {
	ID            int64     `db:"id"`
	Kind          string    `db:"kind"`
	ChannelID     string    `db:"channel_id"`
	ContentID     string    `db:"content_id"`
	Payload       string    `db:"payload"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (deliveryTelemetryTestOutboxModel) TableName() string {
	return testTableOutbox
}

type deliveryTelemetryOutboxSpec struct {
	kind         domain.OutboxKind
	channelID    string
	contentID    string
	payload      string
	status       domain.OutboxStatus
	attemptCount int
	createdAt    time.Time
}

func seedDeliveryTelemetryOutboxes(
	t *testing.T,
	db *pgxpool.Pool,
	nextAttemptAt time.Time,
	specs []deliveryTelemetryOutboxSpec,
) []deliveryTelemetryTestOutboxModel {
	t.Helper()

	rows := make([]deliveryTelemetryTestOutboxModel, 0, len(specs))

	for _, spec := range specs {
		row := deliveryTelemetryTestOutboxModel{
			Kind:          string(spec.kind),
			ChannelID:     spec.channelID,
			ContentID:     spec.contentID,
			Payload:       spec.payload,
			Status:        string(spec.status),
			AttemptCount:  spec.attemptCount,
			NextAttemptAt: nextAttemptAt,
			CreatedAt:     spec.createdAt,
		}

		require.NoError(t, insertDeliveryTestRows(db, &row).Error)

		rows = append(rows, row)
	}

	return rows
}

type deliveryTelemetryTestDeliveryModel struct {
	ID            int64     `db:"id"`
	OutboxID      int64     `db:"outbox_id"`
	RoomID        string    `db:"room_id"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (deliveryTelemetryTestDeliveryModel) TableName() string {
	return testTableDelivery
}

type deliveryTelemetryTestBufferModel struct {
	ID                 int64  `db:"id"`
	DeliveryID         int64  `db:"delivery_id"`
	AttemptOrdinal     int    `db:"attempt_ordinal"`
	OutboxID           int64  `db:"outbox_id"`
	ChannelID          string `db:"channel_id"`
	ContentID          string `db:"content_id"`
	PostID             string `db:"post_id"`
	RoomID             string `db:"room_id"`
	AlarmType          string `db:"alarm_type"`
	ActualPublishedAt  *time.Time
	AlarmSentAt        *time.Time
	AlarmLatencyMillis *int64
	DetectedAt         *time.Time
	DedupeKey          string `db:"dedupe_key"`
	DeliveryPath       string `db:"delivery_path"`
	DeliveryMode       string `db:"delivery_mode"`
	SendResult         string `db:"send_result"`
	FailureReason      string `db:"failure_reason"`
	AttemptStartedAt   *time.Time
	AttemptFinishedAt  *time.Time
	EventAt            time.Time `db:"event_at"`
	NextAttemptAt      time.Time `db:"next_attempt_at"`
	CreatedAt          time.Time
	LockedAt           *time.Time
	LoggedAt           *time.Time
	Error              string `db:"error"`
}

func (deliveryTelemetryTestBufferModel) TableName() string {
	return testTableDeliveryTelemetry
}

type deliveryTelemetryTestAlarmTrackingModel struct {
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

func (deliveryTelemetryTestAlarmTrackingModel) TableName() string {
	return testTableContentAlarmTracking
}

func TestDeliveryTelemetryRepository_BackfillAndFlush(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	delivery, sentAt, alarmLatencyMillis := seedBackfillTelemetryFixture(t, db)

	repository := telemetry.NewRepository(db)

	inserted, err := repository.BackfillFromDelivery(ctx, 10, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	pending, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, delivery.ID, pending[0].DeliveryID)
	require.Equal(t, 1, pending[0].AttemptOrdinal)
	require.Equal(t, sendResultSuccess, pending[0].SendResult)
	require.Equal(t, string(domain.AlarmTypeCommunity), string(pending[0].AlarmType))
	require.Equal(t, telemetry.CommunityShortsDeliveryPath, pending[0].DeliveryPath)
	require.Equal(t, "post-backfill", pending[0].PostID)
	require.Nil(t, pending[0].AttemptStartedAt)
	require.NotNil(t, pending[0].AttemptFinishedAt)
	require.Equal(t, sentAt, pending[0].AttemptFinishedAt.UTC())
	require.NotNil(t, pending[0].AlarmSentAt)
	require.Equal(t, sentAt, pending[0].AlarmSentAt.UTC())
	require.NotNil(t, pending[0].AlarmLatencyMillis)
	require.Equal(t, alarmLatencyMillis, *pending[0].AlarmLatencyMillis)

	require.NoError(t, repository.MarkLoggedBatch(ctx, []int64{pending[0].ID}))

	var saved deliveryTelemetryTestBufferModel

	require.NoError(t, firstDeliveryTestRow(db, &saved, pending[0].ID).Error)
	require.NotNil(t, saved.LoggedAt)
	require.Equal(t, "post-backfill", saved.PostID)
}

func TestDeliveryTelemetryRepository_BackfillFromDelivery_ExecModeEncodesEnumFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	sentAt := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	outbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_backfill_exec",
		ContentID:     "post-backfill-exec",
		Payload:       `{"post_id":"post-backfill-exec","content_text":"hello"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-backfill-exec",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}).Error)

	repository := telemetry.NewRepository(newDeliveryExecModePool(t, db))
	inserted, err := repository.BackfillFromDelivery(ctx, 10, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)
}

func TestDeliveryTelemetryRepository_EnqueueDedupesByDeliveryAttempt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	repository := telemetry.NewRepository(db)
	event := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     101,
		AttemptOrdinal: 1,
		OutboxID:       201,
		ChannelID:      "UC_dedupe",
		ContentID:      testShortOne,
		RoomID:         testRoomOne,
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      testDedupeKeyShortOne,
		DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:   deliveryModePerRoom,
		SendResult:     sendResultSuccess,
		EventAt:        time.Now().UTC(),
	}

	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{event}))
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{event}))

	var count int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &deliveryTelemetryTestBufferModel{}, &count, "").Error)
	require.Equal(t, int64(1), count)

	var saved deliveryTelemetryTestBufferModel

	require.NoError(t, firstDeliveryTestRow(db, &saved).Error)
	require.Equal(t, testShortOne, saved.PostID)
}

func TestDeliveryTelemetryRepository_BackfillFromDelivery_AppliesRetentionCutoff(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC()
	oldSentAt := now.Add(-25 * time.Hour)
	recentSentAt := now.Add(-2 * time.Hour)

	oldOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_old",
		ContentID:     "post-old",
		Payload:       `{"post_id":"post-old","content_text":"old"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: oldSentAt,
		SentAt:        &oldSentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &oldOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestDeliveryModel{
		OutboxID:      oldOutbox.ID,
		RoomID:        testRoomOld,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: oldSentAt,
		CreatedAt:     oldSentAt,
		SentAt:        &oldSentAt,
	}).Error)

	recentOutbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_recent",
		ContentID:     "short-recent",
		Payload:       `{"video_id":"short-recent","title":"recent"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: recentSentAt,
		SentAt:        &recentSentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &recentOutbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestDeliveryModel{
		OutboxID:      recentOutbox.ID,
		RoomID:        "room-recent",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: recentSentAt,
		CreatedAt:     recentSentAt,
		SentAt:        &recentSentAt,
	}).Error)

	repository := telemetry.NewRepository(db)
	inserted, err := repository.BackfillFromDelivery(ctx, 10, now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	var rows []deliveryTelemetryTestBufferModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &rows, "content_id ASC").Error)
	require.Len(t, rows, 1)
	require.Equal(t, "short-recent", rows[0].ContentID)
	require.Equal(t, "short-recent", rows[0].PostID)
}

func TestDispatcher_Cleanup_RemovesOnlyLoggedTelemetryOlderThanRetention(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC()
	oldLoggedAt := now.Add(-25 * time.Hour)
	recentLoggedAt := now.Add(-2 * time.Hour)

	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestOutboxModel{
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

	rows := cleanupRetentionTelemetryRows(now, oldLoggedAt, recentLoggedAt)

	require.NoError(t, insertDeliveryTestRows(db, &rows).Error)

	config := dispatchstate.DefaultConfig()

	config.CleanupAfter = 7 * 24 * time.Hour
	config.CleanupEnabled = false
	config.TelemetryRetention = 24 * time.Hour

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &config)
	dispatcher.CleanupForTest(ctx)

	var remaining []deliveryTelemetryTestBufferModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &remaining, "content_id ASC").Error)
	require.Len(t, remaining, 2)
	require.Equal(t, "old-pending", remaining[0].ContentID)
	require.Equal(t, "recent-logged", remaining[1].ContentID)
}

func TestDispatcher_ProcessDeliveryTelemetry_EmitsBufferedAuditLogs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	sentAt := time.Now().UTC().Add(-time.Minute)
	outbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_emit",
		ContentID:     "short-emit",
		Payload:       `{"video_id":"short-emit","title":"emit"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)

	delivery := deliveryTelemetryTestDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-emit",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	actualPublishedAt := sentAt.Add(-13 * time.Minute).UTC()
	detectedAt := actualPublishedAt.Add(20 * time.Second)
	alarmSentAt := actualPublishedAt.Add(3 * time.Minute)
	alarmLatencyMillis := int64(alarmSentAt.Sub(actualPublishedAt) / time.Millisecond)
	alarmLatencyExceeded := true
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestAlarmTrackingModel{
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
	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, logger, &dispatchstate.Config{
		LockTimeout:            time.Minute,
		TelemetryBackfillBatch: 10,
		TelemetryFlushBatch:    10,
	})

	dispatcher.telemetry.processDeliveryTelemetry(ctx)

	var rows []deliveryTelemetryTestBufferModel

	require.NoError(t, findDeliveryTestRows(db, &rows).Error)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].LoggedAt)
	require.Equal(t, "short-emit", rows[0].PostID)
	require.Contains(t, logBuffer.String(), deliveryAuditLogMessage)
	require.Contains(t, logBuffer.String(), "\"delivery_path\":\""+telemetry.CommunityShortsDeliveryPath+"\"")
	require.Contains(t, logBuffer.String(), "\"post_id\":\"short-emit\"")
	require.Contains(t, logBuffer.String(), "\"actual_published_at\":")
	require.Contains(t, logBuffer.String(), "\"alarm_latency_exceeded\":true")
	require.Contains(t, logBuffer.String(), "\"latency_classification\":{")
	require.Contains(t, logBuffer.String(), "\"delay_source\":")
}

func TestDeliveryTelemetryRepository_MarkRetryReleasesLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	repository := telemetry.NewRepository(db)
	now := time.Now().UTC()
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     501,
		AttemptOrdinal: 1,
		OutboxID:       601,
		ChannelID:      "UC_retry",
		ContentID:      "post-retry",
		RoomID:         "room-retry",
		AlarmType:      domain.AlarmTypeCommunity,
		DedupeKey:      "youtube-notification:COMMUNITY_POST:post-retry",
		DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:   deliveryModeGrouped,
		SendResult:     sendResultFailure,
		FailureReason:  deliveryReasonSendMessage,
		EventAt:        now,
		NextAttemptAt:  now,
	}}))

	locked, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, locked, 1)
	require.NoError(t, repository.MarkRetryBatch(ctx, []int64{locked[0].ID}, time.Millisecond, "emit failed"))

	time.Sleep(2 * time.Millisecond)

	again, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, locked[0].ID, again[0].ID)
	require.Equal(t, "post-retry", again[0].PostID)
	require.NoError(t, repository.MarkLoggedBatch(ctx, []int64{again[0].ID}))
}

var _ = io.Discard

func cleanupRetentionTelemetryRows(now, oldLoggedAt, recentLoggedAt time.Time) []deliveryTelemetryTestBufferModel {
	return []deliveryTelemetryTestBufferModel{
		{
			DeliveryID:     701,
			AttemptOrdinal: 1,
			OutboxID:       1,
			ChannelID:      "UC_cleanup",
			ContentID:      "old-logged",
			PostID:         "old-logged",
			RoomID:         testRoomOld,
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:old-logged",
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
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
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultSuccess,
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
			DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
			DeliveryMode:   deliveryModeGrouped,
			SendResult:     sendResultFailure,
			EventAt:        oldLoggedAt,
			NextAttemptAt:  now,
		},
	}
}

func seedBackfillTelemetryFixture(t *testing.T, db *pgxpool.Pool) (deliveryTelemetryTestDeliveryModel, time.Time, int64) {
	t.Helper()

	sentAt := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	outbox := deliveryTelemetryTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_backfill",
		ContentID:     "post-backfill",
		Payload:       `{"post_id":"post-backfill","content_text":"hello"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)

	delivery := deliveryTelemetryTestDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-backfill",
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt,
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	actualPublishedAt := sentAt.Add(-2 * time.Minute)
	detectedAt := sentAt.Add(-1 * time.Minute)
	alarmLatencyMillis := int64(sentAt.Sub(actualPublishedAt) / time.Millisecond)
	alarmLatencyExceeded := false
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTelemetryTestAlarmTrackingModel{
		Kind:                 string(domain.OutboxKindCommunityPost),
		ContentID:            outbox.ContentID,
		ChannelID:            outbox.ChannelID,
		ActualPublishedAt:    &actualPublishedAt,
		DetectedAt:           detectedAt,
		AlarmSentAt:          &sentAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: &alarmLatencyExceeded,
	}).Error)

	return delivery, sentAt, alarmLatencyMillis
}
