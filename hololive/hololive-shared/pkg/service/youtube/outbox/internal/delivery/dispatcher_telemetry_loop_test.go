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

	repo := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
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

	repo := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repo.Enqueue(ctx, []domain.YouTubeNotificationDeliveryTelemetry{{
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
