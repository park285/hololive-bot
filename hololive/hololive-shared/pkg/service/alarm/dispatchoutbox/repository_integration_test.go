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

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database config: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
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
		"hololive/hololive-kakao-bot-go/scripts/migrations/065_record_alarm_dispatch_event_collisions.sql",
	} {
		sql := readRepoMigration(t, migration)
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return NewPgxRepositoryFromPool(pool, nil), pool
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
	repository, pool := setupDispatchOutboxIntegration(t)
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

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes, Status: StatusPending})
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

func TestPgxRepositoryInsertBatch_RecordsSameBatchHashConflict(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
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

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}, Status: StatusPending})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("InsertedEvents = %d, want 1 committed event", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 2 {
		t.Fatalf("InsertedDeliveries = %d, want 2 (conflicting room re-pointed to winner event)", result.InsertedDeliveries)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 1 || deliveryCount != 2 {
		t.Fatalf("stored counts after conflict events=%d deliveries=%d, want 1/2", eventCount, deliveryCount)
	}

	firstEvent, _, _ := buildLedgerRows(&first, StatusPending)
	var room2Hash string
	if err := pool.QueryRow(ctx, `
		SELECT e.payload_hash
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id = d.event_id
		WHERE d.room_id='room-2'`).Scan(&room2Hash); err != nil {
		t.Fatalf("load room-2 delivery event hash: %v", err)
	}
	if room2Hash != firstEvent.PayloadHash {
		t.Fatalf("room-2 delivery event hash = %q, want winner %q (first-wins content)", room2Hash, firstEvent.PayloadHash)
	}

	var collisionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_event_collisions").Scan(&collisionCount); err != nil {
		t.Fatalf("count collisions: %v", err)
	}
	if collisionCount != 1 {
		t.Fatalf("collision count = %d, want 1", collisionCount)
	}
}

func TestPgxRepositoryInsertBatch_RecordsExistingEventHashConflict(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
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

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first}, Status: StatusPending}); err != nil {
		t.Fatalf("first InsertBatch() error = %v", err)
	}
	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{second}, Status: StatusPending})
	if err != nil {
		t.Fatalf("second InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.ProcessedDeliveries != 1 {
		t.Fatalf("ProcessedDeliveries = %d, want 1", result.ProcessedDeliveries)
	}
	if result.InsertedEvents != 0 {
		t.Fatalf("InsertedEvents = %d, want 0", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 1 {
		t.Fatalf("InsertedDeliveries = %d, want 1 (conflicting room re-pointed to existing event)", result.InsertedDeliveries)
	}

	var eventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d, want 1 (conflict should not create new event)", eventCount)
	}

	var storedHash string
	if err := pool.QueryRow(ctx, "SELECT payload_hash FROM alarm_dispatch_events LIMIT 1").Scan(&storedHash); err != nil {
		t.Fatalf("load payload_hash: %v", err)
	}
	firstEvent, _, _ := buildLedgerRows(&first, StatusPending)
	if storedHash != firstEvent.PayloadHash {
		t.Fatalf("payload_hash = %q, want original %q (conflict upsert should not change payload)", storedHash, firstEvent.PayloadHash)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 2 {
		t.Fatalf("delivery count = %d, want original plus re-pointed conflicting room", deliveryCount)
	}

	var room2Count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-2'").Scan(&room2Count); err != nil {
		t.Fatalf("count room-2 deliveries: %v", err)
	}
	if room2Count != 1 {
		t.Fatalf("room-2 delivery count = %d, want 1 (conflicting room must not be silently lost)", room2Count)
	}

	secondEvent, _, _ := buildLedgerRows(&second, StatusPending)
	var existingHash, incomingHash string
	var collisionPayload []byte
	if err := pool.QueryRow(ctx, `
		SELECT existing_payload_hash, incoming_payload_hash, payload
		FROM alarm_dispatch_event_collisions
		WHERE event_key=$1`, secondEvent.EventKey).Scan(&existingHash, &incomingHash, &collisionPayload); err != nil {
		t.Fatalf("load collision: %v", err)
	}
	if existingHash != firstEvent.PayloadHash || incomingHash != secondEvent.PayloadHash {
		t.Fatalf("collision hashes existing=%q incoming=%q, want %q/%q", existingHash, incomingHash, firstEvent.PayloadHash, secondEvent.PayloadHash)
	}
	if !strings.Contains(string(collisionPayload), "second") {
		t.Fatalf("collision payload = %s, want changed payload title", string(collisionPayload))
	}
}

func TestPgxRepositoryInsertBatch_ExistingConflictRecordsCollisionAndDeliversAllRooms(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	eventA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "original"},
		},
		Version: 1,
	}

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{eventA}, Status: StatusPending}); err != nil {
		t.Fatalf("step 1 InsertBatch() error = %v", err)
	}

	conflictA := eventA
	conflictA.Notification.RoomID = "room-2"
	conflictA.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "changed"}

	eventB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-3",
			Channel:   &domain.Channel{ID: "channel-2"},
			Stream:    &domain.Stream{ID: "stream-2", ChannelID: "channel-2", StartScheduled: &start, Title: "new"},
		},
		Version: 1,
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{conflictA, eventB}, Status: StatusPending})
	if err != nil {
		t.Fatalf("step 2 InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.ProcessedDeliveries != 2 {
		t.Fatalf("ProcessedDeliveries = %d, want 2", result.ProcessedDeliveries)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("InsertedEvents = %d, want 1 committed non-conflicting event", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 2 {
		t.Fatalf("InsertedDeliveries = %d, want 2 (conflicting room re-pointed + non-conflicting room)", result.InsertedDeliveries)
	}

	var eventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("event count = %d, want original plus non-conflicting event", eventCount)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 3 {
		t.Fatalf("delivery count = %d, want original plus conflicting plus non-conflicting delivery", deliveryCount)
	}

	eventAEvent, _, _ := buildLedgerRows(&eventA, StatusPending)
	var room2Count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-2'").Scan(&room2Count); err != nil {
		t.Fatalf("count room-2 deliveries: %v", err)
	}
	if room2Count != 1 {
		t.Fatalf("room-2 delivery count = %d, want 1 (conflicting room must not be silently lost)", room2Count)
	}

	var room2Hash string
	if err := pool.QueryRow(ctx, `
		SELECT e.payload_hash
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id = d.event_id
		WHERE d.room_id='room-2'`).Scan(&room2Hash); err != nil {
		t.Fatalf("load room-2 delivery event hash: %v", err)
	}
	if room2Hash != eventAEvent.PayloadHash {
		t.Fatalf("room-2 delivery event hash = %q, want existing %q (first-wins content)", room2Hash, eventAEvent.PayloadHash)
	}

	var room3Count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-3'").Scan(&room3Count); err != nil {
		t.Fatalf("count room-3 deliveries: %v", err)
	}
	if room3Count != 1 {
		t.Fatalf("room-3 delivery count = %d, want 1", room3Count)
	}

	var collisionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_event_collisions").Scan(&collisionCount); err != nil {
		t.Fatalf("count collisions: %v", err)
	}
	if collisionCount != 1 {
		t.Fatalf("collision count = %d, want 1", collisionCount)
	}
}

func TestPgxRepositoryInsertBatch_CoalescesRepeatedConflictCollisions(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	original := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "original"},
		},
		Version: 1,
	}
	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{original}, Status: StatusPending}); err != nil {
		t.Fatalf("seed InsertBatch() error = %v", err)
	}

	conflictRooms := []string{"room-2", "room-4", "room-5"}
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(conflictRooms))
	for _, roomID := range conflictRooms {
		env := original
		env.Notification.RoomID = roomID
		env.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "changed"}
		envelopes = append(envelopes, env)
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes, Status: StatusPending})
	if err != nil {
		t.Fatalf("conflict InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.ProcessedDeliveries != 3 {
		t.Fatalf("ProcessedDeliveries = %d, want 3", result.ProcessedDeliveries)
	}
	if result.InsertedDeliveries != 3 {
		t.Fatalf("InsertedDeliveries = %d, want 3 (all conflicting rooms re-pointed to existing event)", result.InsertedDeliveries)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 4 {
		t.Fatalf("delivery count = %d, want 4 (seed plus three re-pointed conflicting rooms)", deliveryCount)
	}

	var collisionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_event_collisions").Scan(&collisionCount); err != nil {
		t.Fatalf("count collisions: %v", err)
	}
	if collisionCount != 1 {
		t.Fatalf("collision count = %d, want 1", collisionCount)
	}
}

func TestPgxRepositoryInsertBatch_DedupesMixedHashSameBatchCollisionRecords(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	winner := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	driftedRoom2 := winner
	driftedRoom2.Notification.RoomID = "room-2"
	driftedRoom2.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "second"}
	driftedRoom3 := winner
	driftedRoom3.Notification.RoomID = "room-3"
	driftedRoom3.Notification.Stream = &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start, Title: "second"}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{winner, driftedRoom2, driftedRoom3},
		Status:    StatusPending,
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v (duplicate (event_key, incoming_payload_hash) collision rows must be deduped, not abort the batch)", err)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("InsertedEvents = %d, want 1", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 3 {
		t.Fatalf("InsertedDeliveries = %d, want 3 (all rooms delivered, drifted rooms re-pointed to winner)", result.InsertedDeliveries)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 1 || deliveryCount != 3 {
		t.Fatalf("stored counts events=%d deliveries=%d, want 1/3", eventCount, deliveryCount)
	}

	winnerEvent, _, _ := buildLedgerRows(&winner, StatusPending)
	var distinctHashes int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT e.payload_hash)
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id = d.event_id`).Scan(&distinctHashes); err != nil {
		t.Fatalf("count distinct delivery event hashes: %v", err)
	}
	if distinctHashes != 1 {
		t.Fatalf("distinct delivery event hashes = %d, want 1 (all rooms first-wins)", distinctHashes)
	}
	var storedHash string
	if err := pool.QueryRow(ctx, "SELECT payload_hash FROM alarm_dispatch_events LIMIT 1").Scan(&storedHash); err != nil {
		t.Fatalf("load payload_hash: %v", err)
	}
	if storedHash != winnerEvent.PayloadHash {
		t.Fatalf("stored event hash = %q, want winner %q", storedHash, winnerEvent.PayloadHash)
	}

	var collisionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_event_collisions").Scan(&collisionCount); err != nil {
		t.Fatalf("count collisions: %v", err)
	}
	if collisionCount != 1 {
		t.Fatalf("collision count = %d, want 1 (duplicate collision rows coalesced)", collisionCount)
	}
}

func TestPgxRepositoryInsertBatch_SkipsLegacyDedupeKey(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
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
	event, delivery, err := buildLedgerRows(&envelope, StatusPending)
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

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.InsertedDeliveries != 0 || result.DuplicateDeliveries != 1 {
		t.Fatalf("InsertBatch() result = %+v, want legacy duplicate skip", result)
	}
}

func TestPgxRepositoryInsertBatch_DedupesDuplicateDedupeKeyWithinBatch(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
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

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope, envelope}, Status: StatusPending})
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
	repository, pool := setupDispatchOutboxIntegration(t)
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

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repository.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	if err := repository.ReleaseLeased(ctx, []int64{claimed[0].ID}, "worker-1"); err != nil {
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
	repository, pool := setupDispatchOutboxIntegration(t)
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

	result, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: envelopes,
		Status:    StatusPending,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.InsertedDeliveries)

	claimed, err := repository.ClaimDue(ctx, workerID, 2, time.Minute)
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
	require.NoError(t, repository.ScheduleRetry(ctx, []RetryUpdate{
		{
			ID:            retryID,
			AttemptCount:  1,
			NextAttemptAt: nextAttemptAt,
			Error:         "jsonb retry test",
		},
	}, workerID))

	require.NoError(t, repository.MoveToDLQ(ctx, []TerminalUpdate{
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
	repository, pool := setupDispatchOutboxIntegration(t)
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

	_, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope},
		Status:    StatusPending,
	})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	require.NoError(t, repository.MarkSending(ctx, []int64{id}, workerID, time.Minute))
	require.NoError(t, repository.Quarantine(ctx, []TerminalUpdate{
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

func TestPgxRepositoryScheduleSendingRetry_TransitionsSendingToRetry(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-sending-retry"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-sending-retry",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-sr", ChannelID: "channel-1", StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-sr"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	// leased → sending
	require.NoError(t, repository.MarkSending(ctx, []int64{id}, workerID, time.Minute))

	var statusAfterSending string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", id).Scan(&statusAfterSending))
	require.Equal(t, string(StatusSending), statusAfterSending, "row must be 'sending' before ScheduleSendingRetry")

	nextAttemptAt := time.Now().UTC().Add(5 * time.Second)
	require.NoError(t, repository.ScheduleSendingRetry(ctx, []RetryUpdate{
		{
			ID:            id,
			AttemptCount:  1,
			NextAttemptAt: nextAttemptAt,
			Error:         "502 bad gateway",
		},
	}, workerID))

	var finalStatus string
	var attemptCount int
	var lastError string
	var lockedBy *string
	err = pool.QueryRow(ctx, `
		SELECT status, attempt_count, last_error, locked_by
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&finalStatus, &attemptCount, &lastError, &lockedBy)
	require.NoError(t, err)
	require.Equal(t, string(StatusRetry), finalStatus, "row must transition sending → retry")
	require.Equal(t, 1, attemptCount)
	require.Equal(t, "502 bad gateway", lastError)
	require.Nil(t, lockedBy, "locked_by must be cleared after retry transition")
}

func TestPgxRepositoryScheduleSendingRetry_DoesNotTouchTerminalRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-sending-retry-terminal"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	sent := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-sent",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-sent", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}
	quarantined := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-quarantined",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-quarantined", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{sent, quarantined}, Status: StatusPending})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	var sentID, quarantinedID int64
	for _, r := range claimed {
		switch r.RoomID {
		case "room-sent":
			sentID = r.ID
		case "room-quarantined":
			quarantinedID = r.ID
		}
	}

	require.NoError(t, repository.MarkSending(ctx, []int64{sentID, quarantinedID}, workerID, time.Minute))
	require.NoError(t, repository.MarkSent(ctx, []int64{sentID}, workerID))
	require.NoError(t, repository.Quarantine(ctx, []TerminalUpdate{{ID: quarantinedID, Error: "hard fail"}}, workerID))

	nextAttemptAt := time.Now().UTC().Add(5 * time.Second)
	// ScheduleSendingRetry는 status IN ('leased','sending')만 건드려야 함
	// sent/quarantined row에 대한 update는 0 rows 이지만 expectRowsAffected 로 오류 반환
	err = repository.ScheduleSendingRetry(ctx, []RetryUpdate{
		{ID: sentID, AttemptCount: 1, NextAttemptAt: nextAttemptAt, Error: "ignored"},
		{ID: quarantinedID, AttemptCount: 1, NextAttemptAt: nextAttemptAt, Error: "ignored"},
	}, workerID)
	// 0 rows affected — terminal rows는 건드리지 않으므로 expectRowsAffected 에러 발생 예상
	require.Error(t, err, "ScheduleSendingRetry must return error when no rows match (terminal rows protected)")

	var sentStatus, quarantinedStatus string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", sentID).Scan(&sentStatus))
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", quarantinedID).Scan(&quarantinedStatus))
	require.Equal(t, string(StatusSent), sentStatus, "sent row must remain sent")
	require.Equal(t, string(StatusQuarantined), quarantinedStatus, "quarantined row must remain quarantined")
}

// TestPgxRepositoryScheduleSendingRetry_ExpiredLeaseStillTransitions는
// lock_expires_at이 과거인 'sending' row에 대해서도 소유 worker의 ScheduleSendingRetry가
// 성공적으로 retry로 전환해야 함을 검증한다.
// 만료돼도 locked_by=$workerID이 소유권을 보장하므로 double-send 위험이 없다.
func TestPgxRepositoryScheduleSendingRetry_ExpiredLeaseStillTransitions(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-expired-lease"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-expired",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-expired", ChannelID: "channel-1", StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-expired"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	require.NoError(t, repository.MarkSending(ctx, []int64{id}, workerID, time.Minute))

	_, err = pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	var statusBeforeRetry string
	var expiresAt interface{}
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, lock_expires_at < NOW()
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&statusBeforeRetry, &expiresAt))
	require.Equal(t, string(StatusSending), statusBeforeRetry, "row must be 'sending' with expired lease")

	nextAttemptAt := time.Now().UTC().Add(5 * time.Second)
	err = repository.ScheduleSendingRetry(ctx, []RetryUpdate{
		{
			ID:            id,
			AttemptCount:  1,
			NextAttemptAt: nextAttemptAt,
			Error:         "502 expired lease retry",
		},
	}, workerID)
	require.NoError(t, err, "ScheduleSendingRetry must succeed for 'sending' row with expired lease owned by same worker")

	var finalStatus string
	var lockedBy *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&finalStatus, &lockedBy))
	require.Equal(t, string(StatusRetry), finalStatus, "row must transition sending → retry even with expired lease")
	require.Nil(t, lockedBy, "locked_by must be cleared after retry transition")
}

func TestPgxRepositoryReleaseLeased_RequiresOwner(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
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

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repository.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	if err := repository.ReleaseLeased(ctx, []int64{claimed[0].ID}, "worker-2"); err == nil {
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
