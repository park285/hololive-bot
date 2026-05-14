//go:build integration

package dispatchoutbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func setupDispatchOutboxIntegration(t *testing.T) (*PgxRepository, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	setupPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}

	schema := fmt.Sprintf("dispatchoutbox_test_%d", time.Now().UnixNano())
	if _, err := setupPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	setupPool.Close()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database config: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect schema-scoped test database: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		cleanupPool, err := pgxpool.New(context.Background(), dsn)
		if err == nil {
			_, _ = cleanupPool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			cleanupPool.Close()
		}
	})
	if _, err := pool.Exec(ctx, "CREATE TYPE alarm_type AS ENUM ('LIVE', 'COMMUNITY', 'SHORTS')"); err != nil {
		t.Fatalf("create alarm_type: %v", err)
	}
	for _, migration := range []string{
		"hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql",
		"hololive/hololive-kakao-bot-go/scripts/migrations/059_harden_alarm_dispatch_outbox.sql",
	} {
		sql := readRepoMigration(t, migration)
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return NewPgxRepositoryFromPool(pool), pool
}

func readRepoMigration(t *testing.T, relative string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("read migration %s: %v", relative, err)
	}
	return string(raw)
}

func TestPgxRepositoryInsertBatch_SetBasedPath(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	envelopes := make([]domain.AlarmQueueEnvelope, 0, 1000)
	for i := range 1000 {
		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    fmt.Sprintf("room-%04d", i),
				Channel:   &domain.Channel{ID: "channel-1"},
				Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
				Users:     []string{fmt.Sprintf("user-%04d", i)},
			},
			ClaimKeys: []string{fmt.Sprintf("claim-%04d", i)},
			Version:   1,
		})
	}

	result, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes, Status: StatusPending})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.InsertedEvents != 1 || result.InsertedDeliveries != 1000 {
		t.Fatalf("InsertBatch() result = %+v, want 1 event and 1000 deliveries", result)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 1 || deliveryCount != 1000 {
		t.Fatalf("stored counts events=%d deliveries=%d, want 1/1000", eventCount, deliveryCount)
	}

	var claimKeys []string
	if err := pool.QueryRow(ctx, "SELECT claim_keys FROM alarm_dispatch_deliveries WHERE room_id='room-0007'").Scan(&claimKeys); err != nil {
		t.Fatalf("load claim_keys: %v", err)
	}
	if len(claimKeys) != 1 || claimKeys[0] != "claim-0007" {
		t.Fatalf("claim_keys = %v, want [claim-0007]", claimKeys)
	}
}

func TestPgxRepositoryInsertBatch_RollsBackSameBatchHashConflict(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	first := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	second := first
	second.Notification.RoomID = "room-2"
	second.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "second"}

	_, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}, Status: StatusPending})
	if err == nil || !strings.Contains(err.Error(), "dispatch event hash conflict") {
		t.Fatalf("InsertBatch() error = %v, want hash conflict", err)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 0 || deliveryCount != 0 {
		t.Fatalf("stored counts after conflict events=%d deliveries=%d, want 0/0", eventCount, deliveryCount)
	}
}

func TestPgxRepositoryInsertBatch_RejectsExistingEventHashConflict(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	first := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	second := first
	second.Notification.RoomID = "room-2"
	second.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "second"}

	if _, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first}, Status: StatusPending}); err != nil {
		t.Fatalf("first InsertBatch() error = %v", err)
	}
	_, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{second}, Status: StatusPending})
	if err == nil || !strings.Contains(err.Error(), "dispatch event hash conflict") {
		t.Fatalf("second InsertBatch() error = %v, want hash conflict", err)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("delivery count = %d, want original row only", deliveryCount)
	}
}

func TestPgxRepositoryInsertBatch_SkipsLegacyDedupeKey(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		ClaimKeys: []string{"legacy-category"},
		Version:   1,
	}
	event, delivery, err := buildLedgerRows(envelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows() error = %v", err)
	}
	var eventID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		)
		VALUES ($1, $2, $3::alarm_type, $4, $5, $6, $7)
		RETURNING id`,
		event.EventKey, event.PayloadHash, event.AlarmType, event.ChannelID, event.StreamID, event.Category, event.Payload,
	).Scan(&eventID); err != nil {
		t.Fatalf("insert legacy event: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alarm_dispatch_deliveries (
			event_id, room_id, dedupe_key, claim_keys, delivery_context, status
		)
		VALUES ($1, $2, $3, $4, $5, 'pending')`,
		eventID, delivery.RoomID, delivery.LegacyDedupeKey, delivery.ClaimKeys, delivery.DeliveryContext,
	); err != nil {
		t.Fatalf("insert legacy delivery: %v", err)
	}

	result, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.InsertedDeliveries != 0 || result.DuplicateDeliveries != 1 {
		t.Fatalf("InsertBatch() result = %+v, want legacy duplicate skip", result)
	}
}

func TestPgxRepositoryInsertBatch_DedupesDuplicateDedupeKeyWithinBatch(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}

	result, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope, envelope}, Status: StatusPending})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.InsertedDeliveries != 1 || result.DuplicateDeliveries != 1 {
		t.Fatalf("InsertBatch() result = %+v, want one inserted and one duplicate", result)
	}
	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("delivery count = %d, want 1", deliveryCount)
	}
}

func TestPgxRepositoryInsertBatch_DBPayloadConstraintRejectsDeliveryFields(t *testing.T) {
	_, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		)
		VALUES (
			'bad-event',
			'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
			'LIVE'::alarm_type,
			'channel-1',
			'stream-1',
			'10',
			'{"notification":{"room_id":"room-1"}}'::jsonb
		)`)
	if err == nil {
		t.Fatal("insert room-specific event payload error = nil, want constraint rejection")
	}
}

func TestPgxRepositoryReleaseLeased_RequeuesRows(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}

	if _, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repo.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	if err := repo.ReleaseLeased(ctx, []int64{claimed[0].ID}, "worker-1"); err != nil {
		t.Fatalf("ReleaseLeased() error = %v", err)
	}

	var status string
	var expiresAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT status, lock_expires_at FROM alarm_dispatch_deliveries WHERE id=$1", claimed[0].ID).Scan(&status, &expiresAt); err != nil {
		t.Fatalf("load delivery after release: %v", err)
	}
	if status != string(StatusRetry) || expiresAt != nil {
		t.Fatalf("released row status=%q lock_expires_at=%v, want retry/nil", status, expiresAt)
	}
}

func TestPgxRepositoryJSONBRecordsetParam_RetryAndTerminalBatchPaths(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-jsonb"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelopes := []domain.AlarmQueueEnvelope{
		{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    "room-retry",
				Channel:   &domain.Channel{ID: "channel-1"},
				Stream:    &domain.Stream{ID: "stream-retry", ChannelID: "channel-1", StartScheduled: &start},
			},
			ClaimKeys: []string{"claim-retry"},
			Version:   1,
		},
		{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    "room-dlq",
				Channel:   &domain.Channel{ID: "channel-1"},
				Stream:    &domain.Stream{ID: "stream-dlq", ChannelID: "channel-1", StartScheduled: &start},
			},
			ClaimKeys: []string{"claim-dlq"},
			Version:   1,
		},
	}

	result, err := repo.InsertBatch(ctx, PublishBatchInput{
		Envelopes: envelopes,
		Status:    StatusPending,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.InsertedDeliveries)

	claimed, err := repo.ClaimDue(ctx, workerID, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	var retryID, dlqID int64
	for _, record := range claimed {
		switch record.RoomID {
		case "room-retry":
			retryID = record.ID
		case "room-dlq":
			dlqID = record.ID
		}
	}
	require.NotZero(t, retryID)
	require.NotZero(t, dlqID)

	nextAttemptAt := time.Now().UTC().Add(3 * time.Minute)
	require.NoError(t, repo.ScheduleRetry(ctx, []RetryUpdate{
		{
			ID:            retryID,
			AttemptCount:  1,
			NextAttemptAt: nextAttemptAt,
			Error:         "jsonb retry test",
		},
	}, workerID))

	require.NoError(t, repo.MoveToDLQ(ctx, []TerminalUpdate{
		{
			ID:    dlqID,
			Error: "jsonb dlq test",
		},
	}, workerID))

	var retryStatus string
	var retryAttempt int
	var retryError string
	err = pool.QueryRow(ctx, `
		SELECT status, attempt_count, last_error
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, retryID).Scan(&retryStatus, &retryAttempt, &retryError)
	require.NoError(t, err)
	require.Equal(t, string(StatusRetry), retryStatus)
	require.Equal(t, 1, retryAttempt)
	require.Equal(t, "jsonb retry test", retryError)

	var dlqStatus string
	var dlqError string
	var dlqAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, last_error, dlq_at
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, dlqID).Scan(&dlqStatus, &dlqError, &dlqAt)
	require.NoError(t, err)
	require.Equal(t, string(StatusDLQ), dlqStatus)
	require.Equal(t, "jsonb dlq test", dlqError)
	require.NotNil(t, dlqAt)
}

func TestPgxRepositoryJSONBRecordsetParam_QuarantineSendingPath(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-jsonb-quarantine"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-quarantine",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-quarantine", ChannelID: "channel-1", StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-quarantine"},
		Version:   1,
	}

	_, err := repo.InsertBatch(ctx, PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope},
		Status:    StatusPending,
	})
	require.NoError(t, err)

	claimed, err := repo.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	require.NoError(t, repo.MarkSending(ctx, []int64{id}, workerID, time.Minute))
	require.NoError(t, repo.Quarantine(ctx, []TerminalUpdate{
		{
			ID:    id,
			Error: "jsonb quarantine test",
		},
	}, workerID))

	var status string
	var lastError string
	var quarantinedAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, last_error, quarantined_at
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, id).Scan(&status, &lastError, &quarantinedAt)
	require.NoError(t, err)
	require.Equal(t, string(StatusQuarantined), status)
	require.Equal(t, "jsonb quarantine test", lastError)
	require.NotNil(t, quarantinedAt)
}

func TestPgxRepositoryReleaseLeased_RequiresOwner(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}

	if _, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repo.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	if err := repo.ReleaseLeased(ctx, []int64{claimed[0].ID}, "worker-2"); err == nil {
		t.Fatal("ReleaseLeased() error = nil, want owner mismatch error")
	}

	var status string
	var lockedBy string
	if err := pool.QueryRow(ctx, "SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", claimed[0].ID).Scan(&status, &lockedBy); err != nil {
		t.Fatalf("load delivery after release attempt: %v", err)
	}
	if status != string(StatusLeased) || lockedBy != "worker-1" {
		t.Fatalf("release attempt status=%q locked_by=%q, want leased/worker-1", status, lockedBy)
	}
}
