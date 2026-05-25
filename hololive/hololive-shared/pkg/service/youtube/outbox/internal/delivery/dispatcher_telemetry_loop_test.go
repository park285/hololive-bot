package delivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func openTelemetryLoopTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:dispatcher_telemetry_loop_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return db
}

func TestProcessOnceForTest_DoesNotFlushTelemetryBuffer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTelemetryLoopTestDB(t)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}))

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     701,
		AttemptOrdinal: 1,
		OutboxID:       801,
		ChannelID:      "UC_loop",
		ContentID:      "short-loop",
		RoomID:         "room-loop",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-loop",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Now().UTC(),
		NextAttemptAt:  time.Now().UTC(),
	}}))

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		LockTimeout:           time.Minute,
		PollInterval:          50 * time.Millisecond,
		TelemetryPollInterval: 20 * time.Millisecond,
	})

	dispatcher.ProcessOnceForTest(ctx)

	var rows []sqliteTelemetryBufferModel
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].LoggedAt)
}

func TestDispatcherStart_FlushesTelemetryInBackground(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db := openTelemetryLoopTestDB(t)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}))

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
		DeliveryID:     702,
		AttemptOrdinal: 1,
		OutboxID:       802,
		ChannelID:      "UC_loop_bg",
		ContentID:      "short-loop-bg",
		RoomID:         "room-loop-bg",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-loop-bg",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Now().UTC(),
		NextAttemptAt:  time.Now().UTC(),
	}}))

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		LockTimeout:           time.Minute,
		PollInterval:          10 * time.Millisecond,
		TelemetryPollInterval: 10 * time.Millisecond,
	})

	dispatcher.Start(ctx)

	require.Eventually(t, func() bool {
		var rows []sqliteTelemetryBufferModel
		if err := db.Find(&rows).Error; err != nil || len(rows) != 1 {
			return false
		}
		return rows[0].LoggedAt != nil
	}, 2*time.Second, 25*time.Millisecond)
}

func TestDispatcherTelemetryLoop_ProcessesImmediatelyThenTicksUntilCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	db := openTelemetryLoopTestDB(t)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}))

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{
		telemetryLoopTestRow(703, 803, "short-loop-immediate", "room-loop-immediate"),
	}))

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		LockTimeout:           time.Minute,
		PollInterval:          time.Hour,
		TelemetryPollInterval: 20 * time.Millisecond,
	})

	done := make(chan struct{}, 1)
	go func() {
		dispatcher.telemetry.telemetryLoop(ctx)
		done <- struct{}{}
	}()

	require.Eventually(t, func() bool {
		return telemetryLoopRowLogged(t, db, 703)
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, repository.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{
		telemetryLoopTestRow(704, 804, "short-loop-tick", "room-loop-tick"),
	}))

	require.Eventually(t, func() bool {
		return telemetryLoopRowLogged(t, db, 704)
	}, 2*time.Second, 10*time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("telemetryLoop did not stop after context cancellation")
	}
}

func TestDispatcherTelemetryLoop_StopsOnContextCancelBeforeNextTick(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	db := openTelemetryLoopTestDB(t)
	require.NoError(t, db.AutoMigrate(&sqliteTelemetryOutboxModel{}, &sqliteTelemetryDeliveryModel{}, &sqliteTelemetryBufferModel{}, &sqliteTelemetryObservationTrackingModel{}))

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		LockTimeout:           time.Minute,
		PollInterval:          time.Hour,
		TelemetryPollInterval: time.Hour,
	})

	done := make(chan struct{}, 1)
	go func() {
		dispatcher.telemetry.telemetryLoop(ctx)
		done <- struct{}{}
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("telemetryLoop did not stop after context cancellation")
	}
}

func telemetryLoopTestRow(deliveryID, outboxID int64, contentID, roomID string) domain.YouTubeNotificationDeliveryTelemetry {
	return domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     deliveryID,
		AttemptOrdinal: 1,
		OutboxID:       outboxID,
		ChannelID:      "UC_loop",
		ContentID:      contentID,
		RoomID:         roomID,
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      fmt.Sprintf("youtube-notification:NEW_SHORT:%s", contentID),
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Now().UTC(),
		NextAttemptAt:  time.Now().UTC(),
	}
}

func telemetryLoopRowLogged(t *testing.T, db *gorm.DB, deliveryID int64) bool {
	t.Helper()

	var row sqliteTelemetryBufferModel
	err := db.Where("delivery_id = ?", deliveryID).First(&row).Error
	if err != nil {
		return false
	}
	return row.LoggedAt != nil
}
