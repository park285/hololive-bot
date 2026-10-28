//go:build integration

package dispatchoutbox

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// setupDispatchOutboxIntegration은 production과 같은 전체 migration 사슬을 적용한 격리 DB를 쓴다.
func setupDispatchOutboxIntegration(t *testing.T) (*PgxRepository, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)

	return newTestRepository(t, pool), pool
}

func TestPgxRepositoryInsertBatchKeepsCanonicalPayloadAfterConflictingCandidate(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	seed := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive, RoomID: testRoomID, Channel: &domain.Channel{ID: testChannelID},
			Stream: &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "winner"},
		},
		Version: 1,
	}
	_, _, err := repository.InsertPending(t.Context(), &seed)
	require.NoError(t, err)
	conflict := seed
	conflict.Notification.RoomID = testOtherRoomID
	conflict.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "conflicting"}
	matching := seed
	matching.Notification.RoomID = testOtherRoomID
	result, err := repository.InsertBatch(t.Context(), PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{conflict, matching},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.HashConflictEvents)
	require.Equal(t, 1, result.InsertedDeliveries)
	var payload []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT e.payload FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id=d.event_id WHERE d.room_id=$1`, testOtherRoomID).Scan(&payload))
	require.Contains(t, string(payload), "winner")
	require.NotContains(t, string(payload), "conflicting")
	record, _, err := repository.InsertPending(t.Context(), &conflict)
	require.ErrorIs(t, err, ErrEventPayloadConflict)
	require.Nil(t, record, "a rejected single entry must not receive the winner record")
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
				Channel:   &domain.Channel{ID: testChannelID},
				Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
				Users:     []string{fmt.Sprintf("user-%04d", i)},
			},
			ClaimKeys: []string{fmt.Sprintf("claim-%04d", i)},
			Version:   1,
		})
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes})
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
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	second := first
	second.Notification.RoomID = testOtherRoomID
	second.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "second"}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("InsertedEvents = %d, want 1 committed event", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 1 {
		t.Fatalf("InsertedDeliveries = %d, want 1 (conflicting payload excluded)", result.InsertedDeliveries)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 1 || deliveryCount != 1 {
		t.Fatalf("stored counts after conflict events=%d deliveries=%d, want 1/1", eventCount, deliveryCount)
	}

	var rejectedRoom int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-2'").Scan(&rejectedRoom))
	require.Zero(t, rejectedRoom, "conflicting payload must never become sender input")

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
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	second := first
	second.Notification.RoomID = testOtherRoomID
	second.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "second"}

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first}}); err != nil {
		t.Fatalf("first InsertBatch() error = %v", err)
	}
	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{second}})
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
	if result.InsertedDeliveries != 0 {
		t.Fatalf("InsertedDeliveries = %d, want 0 (conflicting payload excluded)", result.InsertedDeliveries)
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
	firstEvent, _, _ := buildLedgerRows(&first)
	if storedHash != firstEvent.PayloadHash {
		t.Fatalf("payload_hash = %q, want original %q (conflict upsert should not change payload)", storedHash, firstEvent.PayloadHash)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("delivery count = %d, want original delivery only", deliveryCount)
	}

	var room2Count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-2'").Scan(&room2Count); err != nil {
		t.Fatalf("count room-2 deliveries: %v", err)
	}
	if room2Count != 0 {
		t.Fatalf("room-2 delivery count = %d, want 0 (conflicting payload cannot be delivered)", room2Count)
	}

	secondEvent, _, _ := buildLedgerRows(&second)
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

func TestPgxRepositoryInsertBatch_ExistingConflictRecordsCollisionAndDeliversOnlyMatchingPayloads(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	eventA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "original"},
		},
		Version: 1,
	}

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{eventA}}); err != nil {
		t.Fatalf("step 1 InsertBatch() error = %v", err)
	}

	conflictA := eventA
	conflictA.Notification.RoomID = testOtherRoomID
	conflictA.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "changed"}

	eventB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-3",
			Channel:   &domain.Channel{ID: "channel-2"},
			Stream:    &domain.Stream{ID: "stream-2", ChannelID: "channel-2", StartScheduled: &start, Title: "new"},
		},
		Version: 1,
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{conflictA, eventB}})
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
	if result.InsertedDeliveries != 1 {
		t.Fatalf("InsertedDeliveries = %d, want 1 (only non-conflicting room)", result.InsertedDeliveries)
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
	if deliveryCount != 2 {
		t.Fatalf("delivery count = %d, want original plus non-conflicting delivery", deliveryCount)
	}

	var rejectedRoom int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-2'").Scan(&rejectedRoom))
	require.Zero(t, rejectedRoom)

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
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "original"},
		},
		Version: 1,
	}
	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{original}}); err != nil {
		t.Fatalf("seed InsertBatch() error = %v", err)
	}

	conflictRooms := []string{testOtherRoomID, "room-4", "room-5"}
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(conflictRooms))
	for _, roomID := range conflictRooms {
		env := original
		env.Notification.RoomID = roomID
		env.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "changed"}
		envelopes = append(envelopes, env)
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes})
	if err != nil {
		t.Fatalf("conflict InsertBatch() error = %v", err)
	}
	if result.HashConflictEvents != 1 {
		t.Fatalf("HashConflictEvents = %d, want 1", result.HashConflictEvents)
	}
	if result.ProcessedDeliveries != 3 {
		t.Fatalf("ProcessedDeliveries = %d, want 3", result.ProcessedDeliveries)
	}
	if result.InsertedDeliveries != 0 {
		t.Fatalf("InsertedDeliveries = %d, want 0 (all conflicting payloads excluded)", result.InsertedDeliveries)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("delivery count = %d, want 1 (seed only)", deliveryCount)
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
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "first"},
		},
		Version: 1,
	}
	driftedRoom2 := winner
	driftedRoom2.Notification.RoomID = testOtherRoomID
	driftedRoom2.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "second"}
	driftedRoom3 := winner
	driftedRoom3.Notification.RoomID = "room-3"
	driftedRoom3.Notification.Stream = &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start, Title: "second"}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{winner, driftedRoom2, driftedRoom3},
	})
	if err != nil {
		t.Fatalf("InsertBatch() error = %v (duplicate (event_key, incoming_payload_hash) collision rows must be deduped, not abort the batch)", err)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("InsertedEvents = %d, want 1", result.InsertedEvents)
	}
	if result.InsertedDeliveries != 1 {
		t.Fatalf("InsertedDeliveries = %d, want 1 (only matching payload delivered)", result.InsertedDeliveries)
	}

	var eventCount, deliveryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if eventCount != 1 || deliveryCount != 1 {
		t.Fatalf("stored counts events=%d deliveries=%d, want 1/1", eventCount, deliveryCount)
	}

	winnerEvent, _, _ := buildLedgerRows(&winner)
	var distinctHashes int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT e.payload_hash)
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id = d.event_id`).Scan(&distinctHashes); err != nil {
		t.Fatalf("count distinct delivery event hashes: %v", err)
	}
	if distinctHashes != 1 {
		t.Fatalf("distinct delivery event hashes = %d, want 1 (only winner payload)", distinctHashes)
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

func TestPgxRepositoryInsertBatch_DedupesDuplicateDedupeKeyWithinBatch(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope, envelope}})
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
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repository.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	if err := repository.ReleaseLeased(ctx, recordClaims(claimed[0]), "worker-1"); err != nil {
		t.Fatalf("ReleaseLeased() error = %v", err)
	}

	var status, lastError, lastErrorCode string
	var expiresAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT status, lock_expires_at, last_error, last_error_code FROM alarm_dispatch_deliveries WHERE id=$1", claimed[0].ID).Scan(&status, &expiresAt, &lastError, &lastErrorCode); err != nil {
		t.Fatalf("load delivery after release: %v", err)
	}
	if status != string(StatusRetry) || expiresAt != nil {
		t.Fatalf("released row status=%q lock_expires_at=%v, want retry/nil", status, expiresAt)
	}
	require.Equal(t, "lease released before external send", lastError)
	require.Equal(t, "lease_released", lastErrorCode, "ReleaseLeased must pair its static error code")
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
				Channel:   &domain.Channel{ID: testChannelID},
				Stream:    &domain.Stream{ID: "stream-retry", ChannelID: testChannelID, StartScheduled: &start},
			},
			ClaimKeys: []string{"claim-retry"},
			Version:   1,
		},
		{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    "room-dlq",
				Channel:   &domain.Channel{ID: testChannelID},
				Stream:    &domain.Stream{ID: "stream-dlq", ChannelID: testChannelID, StartScheduled: &start},
			},
			ClaimKeys: []string{"claim-dlq"},
			Version:   1,
		},
	}

	result, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: envelopes,
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

	require.NoError(t, repository.RouteFailures(ctx, []FailureUpdate{
		{
			ID:           retryID,
			AttemptCount: 1,
			RetryAfter:   3 * time.Minute,
			Error:        "jsonb retry test",
			ErrorCode:    ErrorCodeTimeout,
			TargetStatus: StatusRetry,
		},
	}, workerID))

	require.NoError(t, repository.MoveToDLQ(ctx, []TerminalUpdate{
		{
			ID:        dlqID,
			Error:     "jsonb dlq test",
			ErrorCode: ErrorCodePayload,
		},
	}, workerID))

	var retryStatus string
	var retryAttempt int
	var retryError, retryErrorCode string
	err = pool.QueryRow(ctx, `
		SELECT status, attempt_count, last_error, last_error_code
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, retryID).Scan(&retryStatus, &retryAttempt, &retryError, &retryErrorCode)
	require.NoError(t, err)
	require.Equal(t, string(StatusRetry), retryStatus)
	require.Equal(t, 1, retryAttempt)
	require.Equal(t, "jsonb retry test", retryError)
	require.Equal(t, ErrorCodeTimeout, retryErrorCode)

	var dlqStatus string
	var dlqError, dlqErrorCode string
	var dlqAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, last_error, last_error_code, dlq_at
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, dlqID).Scan(&dlqStatus, &dlqError, &dlqErrorCode, &dlqAt)
	require.NoError(t, err)
	require.Equal(t, string(StatusDLQ), dlqStatus)
	require.Equal(t, "jsonb dlq test", dlqError)
	require.Equal(t, ErrorCodePayload, dlqErrorCode)
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
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-quarantine", ChannelID: testChannelID, StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-quarantine"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope},
	})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	beginSendingFixture(t, repository, workerID, claimed[0])
	require.NoError(t, repository.Quarantine(ctx, []TerminalUpdate{
		{
			ID:        id,
			Error:     "jsonb quarantine test",
			ErrorCode: ErrorCodeHTTP5xx,
		},
	}, workerID))

	var status string
	var lastError, lastErrorCode string
	var quarantinedAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, last_error, last_error_code, quarantined_at
		FROM alarm_dispatch_deliveries
		WHERE id=$1
	`, id).Scan(&status, &lastError, &lastErrorCode, &quarantinedAt)
	require.NoError(t, err)
	require.Equal(t, string(StatusQuarantined), status)
	require.Equal(t, "jsonb quarantine test", lastError)
	require.Equal(t, ErrorCodeHTTP5xx, lastErrorCode)
	require.NotNil(t, quarantinedAt)
}

func TestPgxRepositoryRouteSendingFailures_TransitionsSendingToRetry(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-sending-retry"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-sending-retry",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-sr", ChannelID: testChannelID, StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-sr"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	beginSendingFixture(t, repository, workerID, claimed[0])

	var statusAfterSending string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", id).Scan(&statusAfterSending))
	require.Equal(t, string(StatusSending), statusAfterSending, "row must be 'sending' before RouteSendingFailures")

	require.NoError(t, repository.RouteSendingFailures(ctx, []FailureUpdate{
		{
			ID:           id,
			AttemptCount: 1,
			RetryAfter:   5 * time.Second,
			Error:        "502 bad gateway",
			ErrorCode:    ErrorCodeHTTP5xx,
			TargetStatus: StatusRetry,
		},
	}, workerID))

	var finalStatus string
	var attemptCount int
	var lastError, lastErrorCode string
	var lockedBy *string
	err = pool.QueryRow(ctx, `
		SELECT status, attempt_count, last_error, last_error_code, locked_by
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&finalStatus, &attemptCount, &lastError, &lastErrorCode, &lockedBy)
	require.NoError(t, err)
	require.Equal(t, string(StatusRetry), finalStatus, "row must transition sending → retry")
	require.Equal(t, 1, attemptCount)
	require.Equal(t, "502 bad gateway", lastError)
	require.Equal(t, ErrorCodeHTTP5xx, lastErrorCode)
	require.Nil(t, lockedBy, "locked_by must be cleared after retry transition")
}

func TestPgxRepositoryRouteSendingFailures_DoesNotTouchTerminalRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-sending-retry-terminal"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	sent := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-sent",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-sent", ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}
	quarantined := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-quarantined",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-quarantined", ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{sent, quarantined}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	var sentID, sentUnit, quarantinedID int64
	for _, r := range claimed {
		switch r.RoomID {
		case "room-sent":
			sentID, sentUnit = r.ID, r.SendUnitID
		case "room-quarantined":
			quarantinedID = r.ID
		}
	}

	beginSendingFixture(t, repository, workerID, claimed...)
	require.NoError(t, repository.MarkSent(ctx, sentUnit, attemptClaims(0, sentID), workerID, "iris-request", time.Second))
	require.NoError(t, repository.Quarantine(ctx, []TerminalUpdate{{ID: quarantinedID, Error: "hard fail"}}, workerID))

	err = repository.RouteSendingFailures(ctx, []FailureUpdate{
		{ID: sentID, AttemptCount: 1, RetryAfter: 5 * time.Second, Error: "ignored", TargetStatus: StatusRetry},
		{ID: quarantinedID, AttemptCount: 1, RetryAfter: 5 * time.Second, Error: "ignored", TargetStatus: StatusRetry},
	}, workerID)
	miss := requireFenceMiss(t, err, sendfence.ReasonStatusChanged)
	require.Zero(t, miss.Applied)
	require.ElementsMatch(t, []int64{sentID, quarantinedID}, miss.UnappliedDeliveryIDs())

	var sentStatus, quarantinedStatus string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", sentID).Scan(&sentStatus))
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", quarantinedID).Scan(&quarantinedStatus))
	require.Equal(t, string(StatusSent), sentStatus, "sent row must remain sent")
	require.Equal(t, string(StatusQuarantined), quarantinedStatus, "quarantined row must remain quarantined")
}

// lock_expires_at이 과거인 'sending' row에 대해서도 소유 worker의 RouteSendingFailures가
// retry로 전환해야 한다. 만료돼도 locked_by=$workerID이 소유권을 보장하므로 double-send 위험이 없다.
func TestPgxRepositoryRouteSendingFailures_ExpiredLeaseStillTransitions(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-expired-lease"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-expired",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-expired", ChannelID: testChannelID, StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-expired"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	beginSendingFixture(t, repository, workerID, claimed[0])

	_, err = pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	var statusBeforeRetry string
	var expiresAt any
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, lock_expires_at < NOW()
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&statusBeforeRetry, &expiresAt))
	require.Equal(t, string(StatusSending), statusBeforeRetry, "row must be 'sending' with expired lease")

	err = repository.RouteSendingFailures(ctx, []FailureUpdate{
		{
			ID:           id,
			AttemptCount: 1,
			RetryAfter:   5 * time.Second,
			Error:        "502 expired lease retry",
			TargetStatus: StatusRetry,
		},
	}, workerID)
	require.NoError(t, err, "RouteSendingFailures must succeed for 'sending' row with expired lease owned by same worker")

	var finalStatus string
	var lockedBy *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&finalStatus, &lockedBy))
	require.Equal(t, string(StatusRetry), finalStatus, "row must transition sending → retry even with expired lease")
	require.Nil(t, lockedBy, "locked_by must be cleared after retry transition")
}

func TestPgxRepositoryMarkSent_ExpiredLeaseStillTransitions(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-marksent-expired"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-marksent-expired",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-mse", ChannelID: testChannelID, StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-mse"},
		Version:   1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	beginSendingFixture(t, repository, workerID, claimed[0])

	_, err = pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	require.NoError(t, repository.MarkSent(ctx, claimed[0].SendUnitID, recordClaims(claimed[0]), workerID, "iris-request", time.Second))

	var status string
	var lockedBy *string
	var sentAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, locked_by, sent_at FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&status, &lockedBy, &sentAt))
	require.Equal(t, string(StatusSent), status, "row must transition sending → sent even with expired lease")
	require.Nil(t, lockedBy, "locked_by must be cleared after sent transition")
	require.NotNil(t, sentAt, "sent_at must be recorded")
}

func TestPgxRepositoryMarkSent_RequiresOwner(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-marksent-owner",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-mso", ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(ctx, "worker-owner", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	id := claimed[0].ID
	beginSendingFixture(t, repository, "worker-owner", claimed[0])
	err = repository.MarkSent(ctx, claimed[0].SendUnitID, recordClaims(claimed[0]), "worker-intruder", "iris-request", time.Second)
	miss := requireFenceMiss(t, err, sendfence.ReasonOwnerChanged)
	require.Zero(t, miss.Applied)
	require.Equal(t, 1, miss.Expected)

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", id).Scan(&status))
	require.Equal(t, string(StatusSending), status, "non-owner MarkSent must not transition the row")
}

func TestPgxRepositoryMarkSent_AfterStaleSendingQuarantineReturnsFenceMiss(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-quarantine-race"
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-quarantine-race",
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: "stream-qr", ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	id := claimed[0].ID
	beginSendingFixture(t, repository, workerID, claimed[0])

	_, err = pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET sending_started_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)
	quarantined, err := repository.QuarantineStaleSending(ctx, time.Second, 10)
	require.NoError(t, err)
	require.Equal(t, 1, quarantined)

	err = repository.MarkSent(ctx, claimed[0].SendUnitID, recordClaims(claimed[0]), workerID, "iris-request", time.Second)
	miss := requireFenceMiss(t, err, sendfence.ReasonStatusChanged)
	require.Zero(t, miss.Applied)
	require.Equal(t, 1, miss.Expected)

	var status string
	var lockedBy *string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &lockedBy))
	require.Equal(t, string(StatusQuarantined), status)
	require.Nil(t, lockedBy)
}

func TestPgxRepositoryReleaseLeased_RequiresOwner(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	if _, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}}); err != nil {
		t.Fatalf("InsertBatch() error = %v", err)
	}
	claimed, err := repository.ClaimDue(ctx, "worker-1", 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimDue() rows = %d, want 1", len(claimed))
	}
	err = repository.ReleaseLeased(ctx, recordClaims(claimed[0]), "worker-2")
	requireFenceMiss(t, err, sendfence.ReasonOwnerChanged)

	var status string
	var lockedBy string
	if err := pool.QueryRow(ctx, "SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", claimed[0].ID).Scan(&status, &lockedBy); err != nil {
		t.Fatalf("load delivery after release attempt: %v", err)
	}
	if status != string(StatusLeased) || lockedBy != "worker-1" {
		t.Fatalf("release attempt status=%q locked_by=%q, want leased/worker-1", status, lockedBy)
	}
}

func TestPgxRepositoryClaimDue_ConcurrentWorkersClaimDisjointRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	const totalDeliveries = 10
	envelopes := make([]domain.AlarmQueueEnvelope, 0, totalDeliveries)
	for i := range totalDeliveries {
		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    fmt.Sprintf("room-concurrent-%02d", i),
				Channel:   &domain.Channel{ID: "channel-concurrent"},
				Stream: &domain.Stream{
					ID:             fmt.Sprintf("stream-concurrent-%02d", i),
					ChannelID:      "channel-concurrent",
					StartScheduled: &start,
				},
			},
			ClaimKeys: []string{fmt.Sprintf("claim-concurrent-%02d", i)},
			Version:   1,
		})
	}

	insertResult, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes})
	require.NoError(t, err)
	require.Equal(t, totalDeliveries, insertResult.InsertedDeliveries)

	workerIDs := []string{"worker-1", "worker-2"}
	claimedByWorker := make([][]*Record, len(workerIDs))
	claimErrByWorker := make([]error, len(workerIDs))

	release := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(len(workerIDs))
	done.Add(len(workerIDs))
	for index, workerID := range workerIDs {
		go func() {
			defer done.Done()
			ready.Done()
			<-release
			claimedByWorker[index], claimErrByWorker[index] = repository.ClaimDue(ctx, workerID, totalDeliveries, time.Minute)
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()

	ownerByDeliveryID := make(map[int64]string, totalDeliveries)
	for index, workerID := range workerIDs {
		require.NoErrorf(t, claimErrByWorker[index], "%s ClaimDue must not fail under concurrent claiming", workerID)
		for _, record := range claimedByWorker[index] {
			previousOwner, alreadyClaimed := ownerByDeliveryID[record.ID]
			require.Falsef(t, alreadyClaimed,
				"delivery %d claimed by both %s and %s — each row must be claimed by exactly one worker",
				record.ID, previousOwner, workerID)
			ownerByDeliveryID[record.ID] = workerID
			require.Equal(t, workerID, record.LockedBy, "returned record must carry the claiming worker as owner")
			require.Equal(t, StatusLeased, record.Status)
		}
	}
	require.Len(t, ownerByDeliveryID, totalDeliveries,
		"concurrent workers must jointly claim every due row exactly once (union of claimed IDs)")

	var leasedCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM alarm_dispatch_deliveries WHERE status=$1", string(StatusLeased),
	).Scan(&leasedCount))
	require.Equal(t, totalDeliveries, leasedCount)

	for deliveryID, workerID := range ownerByDeliveryID {
		var storedStatus, storedLockedBy string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", deliveryID,
		).Scan(&storedStatus, &storedLockedBy))
		require.Equal(t, string(StatusLeased), storedStatus)
		require.Equalf(t, workerID, storedLockedBy,
			"delivery %d locked_by must match the worker whose ClaimDue returned it", deliveryID)
	}
}

func TestPgxRepositoryClaimDue_ConcurrentWorkersKeepOneCanonicalGroupAtomic(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	const roomID = "room-canonical-group"
	starts := []time.Time{
		time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 30, 20, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 30, 41, 0, time.UTC),
	}
	minuteBucket := starts[0].UTC().Truncate(time.Minute)
	for _, start := range starts {
		require.Equal(t, minuteBucket, start.UTC().Truncate(time.Minute),
			"fixture must keep every delivery inside one minute bucket")
	}

	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(starts))
	dedupeKeys := make(map[string]struct{}, len(starts))
	for i, start := range starts {
		envelope := domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    roomID,
				Channel:   &domain.Channel{ID: fmt.Sprintf("channel-group-%d", i)},
				Stream: &domain.Stream{
					ID:             fmt.Sprintf("stream-group-%d", i),
					ChannelID:      fmt.Sprintf("channel-group-%d", i),
					StartScheduled: &start,
				},
			},
			Version: 1,
		}
		dedupeKeys[BuildDedupeKeyFromEnvelope(&envelope)] = struct{}{}
		envelopes = append(envelopes, envelope)
	}
	require.Len(t, dedupeKeys, len(starts), "fixture rows must not dedupe into each other")

	insertResult, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes})
	require.NoError(t, err)
	require.Equal(t, len(starts), insertResult.InsertedDeliveries)

	const perWorkerLimit = 2
	workerIDs := []string{"worker-1", "worker-2"}
	claimedByWorker := make([][]*Record, len(workerIDs))
	claimErrByWorker := make([]error, len(workerIDs))

	release := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(len(workerIDs))
	done.Add(len(workerIDs))
	for index, workerID := range workerIDs {
		go func() {
			defer done.Done()
			ready.Done()
			<-release
			claimedByWorker[index], claimErrByWorker[index] = repository.ClaimDue(ctx, workerID, perWorkerLimit, time.Minute)
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()

	ownerByDeliveryID := make(map[int64]string, len(starts))
	claimingWorkers := 0
	for index, workerID := range workerIDs {
		require.NoErrorf(t, claimErrByWorker[index], "%s ClaimDue must not fail under concurrent claiming", workerID)
		if len(claimedByWorker[index]) > 0 {
			claimingWorkers++
		}
		for _, record := range claimedByWorker[index] {
			previousOwner, alreadyClaimed := ownerByDeliveryID[record.ID]
			require.Falsef(t, alreadyClaimed,
				"delivery %d claimed by both %s and %s — each row must be claimed by exactly one worker",
				record.ID, previousOwner, workerID)
			ownerByDeliveryID[record.ID] = workerID
			require.Equal(t, workerID, record.LockedBy, "returned record must carry the claiming worker as owner")
			require.Equal(t, StatusLeased, record.Status)
		}
	}
	require.Len(t, ownerByDeliveryID, len(starts),
		"concurrent workers must jointly claim every row of the canonical group exactly once")
	require.Equal(t, 1, claimingWorkers,
		"one persisted send unit must be owned by exactly one concurrent worker")

	claimedIDs := make([]int64, 0, len(ownerByDeliveryID))
	for deliveryID := range ownerByDeliveryID {
		claimedIDs = append(claimedIDs, deliveryID)
	}
	var groupRows int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM alarm_dispatch_deliveries WHERE id = ANY($1) AND room_id = $2", claimedIDs, roomID,
	).Scan(&groupRows))
	require.Equal(t, len(starts), groupRows,
		"all rows claimed by one worker must belong to the single persisted send unit")

	for deliveryID, workerID := range ownerByDeliveryID {
		var storedStatus, storedLockedBy string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", deliveryID,
		).Scan(&storedStatus, &storedLockedBy))
		require.Equal(t, string(StatusLeased), storedStatus)
		require.Equalf(t, workerID, storedLockedBy,
			"delivery %d locked_by must match the worker whose ClaimDue returned it", deliveryID)
	}
}

func insertAndClaimRoutingRow(t *testing.T, repository *PgxRepository, workerID, roomID, streamID string) int64 {
	t.Helper()

	return insertAndClaimRoutingRecord(t, repository, workerID, roomID, streamID).ID
}

func insertAndClaimRoutingRecord(t *testing.T, repository *PgxRepository, workerID, roomID, streamID string) *Record {
	t.Helper()
	ctx := context.Background()
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    roomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: streamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		ClaimKeys: []string{"claim-" + roomID},
		Version:   1,
	}
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	claimed, err := repository.ClaimDue(ctx, workerID, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	return claimed[0]
}

func TestPgxRepositoryRouteFailures_AttemptCASRejectsStaleAndSkippedInputs(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-cas"
	id := insertAndClaimRoutingRow(t, repository, workerID, "room-cas", "stream-cas")

	for _, badAttempt := range []int{0, 2} {
		err := repository.RouteFailures(ctx, []FailureUpdate{
			{ID: id, AttemptCount: badAttempt, RetryAfter: time.Minute, Error: "cas mismatch", TargetStatus: StatusRetry},
		}, workerID)
		miss := requireFenceMiss(t, err, sendfence.ReasonVersionChanged)
		require.Equal(t, []int64{id}, miss.UnappliedDeliveryIDs())

		var status string
		var attemptCount int
		var lockedBy *string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, attempt_count, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", id,
		).Scan(&status, &attemptCount, &lockedBy))
		require.Equal(t, string(StatusLeased), status, "CAS-mismatched row must stay leased")
		require.Equal(t, 0, attemptCount, "CAS-mismatched row must keep its attempt count")
		require.NotNil(t, lockedBy)
		require.Equal(t, workerID, *lockedBy)
	}

	require.NoError(t, repository.RouteFailures(ctx, []FailureUpdate{
		{ID: id, AttemptCount: 1, RetryAfter: time.Minute, Error: "cas ok", TargetStatus: StatusRetry},
	}, workerID))
	var status string
	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, attempt_count FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &attemptCount))
	require.Equal(t, string(StatusRetry), status)
	require.Equal(t, 1, attemptCount)
}

func TestPgxRepositoryRouteFailures_MixedRetryAndDLQBatchSingleCall(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-mixed"
	retryID := insertAndClaimRoutingRow(t, repository, workerID, "room-mixed-retry", "stream-mixed-retry")
	dlqID := insertAndClaimRoutingRow(t, repository, workerID, "room-mixed-dlq", "stream-mixed-dlq")

	_, err := pool.Exec(ctx, "UPDATE alarm_dispatch_deliveries SET attempt_count=2 WHERE id=$1", dlqID)
	require.NoError(t, err)

	require.NoError(t, repository.RouteFailures(ctx, []FailureUpdate{
		{ID: retryID, AttemptCount: 1, RetryAfter: 2 * time.Minute, Error: "retry me", TargetStatus: StatusRetry},
		{ID: dlqID, AttemptCount: 3, RetryAfter: 2 * time.Minute, Error: "exhausted", TargetStatus: StatusDLQ},
	}, workerID))

	var retryStatus, retryError string
	var retryAttempt int
	var retryDLQAt *time.Time
	var retryLockedBy *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_error, attempt_count, dlq_at, locked_by
		FROM alarm_dispatch_deliveries WHERE id=$1`, retryID,
	).Scan(&retryStatus, &retryError, &retryAttempt, &retryDLQAt, &retryLockedBy))
	require.Equal(t, string(StatusRetry), retryStatus)
	require.Equal(t, "retry me", retryError)
	require.Equal(t, 1, retryAttempt)
	require.Nil(t, retryDLQAt)
	require.Nil(t, retryLockedBy)

	var dlqStatus, dlqError string
	var dlqAttempt int
	var dlqAt *time.Time
	var dlqLockedBy *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_error, attempt_count, dlq_at, locked_by
		FROM alarm_dispatch_deliveries WHERE id=$1`, dlqID,
	).Scan(&dlqStatus, &dlqError, &dlqAttempt, &dlqAt, &dlqLockedBy))
	require.Equal(t, string(StatusDLQ), dlqStatus)
	require.Equal(t, "exhausted", dlqError)
	require.Equal(t, 3, dlqAttempt, "DLQ terminal row must record the final attempt count")
	require.NotNil(t, dlqAt, "DLQ row must record dlq_at")
	require.Nil(t, dlqLockedBy)
}

func TestPgxRepositoryRouteFailures_PartialBatchAppliesMatchingRowsOnly(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-partial"
	appliedID := insertAndClaimRoutingRow(t, repository, workerID, "room-partial-ok", "stream-partial-ok")
	staleID := insertAndClaimRoutingRow(t, repository, workerID, "room-partial-stale", "stream-partial-stale")

	err := repository.RouteFailures(ctx, []FailureUpdate{
		{ID: appliedID, AttemptCount: 1, RetryAfter: time.Minute, Error: "applied", TargetStatus: StatusRetry},
		{ID: staleID, AttemptCount: 3, RetryAfter: time.Minute, Error: "stale", TargetStatus: StatusDLQ},
	}, workerID)

	miss := requireFenceMiss(t, err, sendfence.ReasonVersionChanged)
	require.Equal(t, 1, miss.Applied)
	require.Equal(t, []int64{staleID}, miss.UnappliedDeliveryIDs())

	var appliedStatus, staleStatus string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", appliedID).Scan(&appliedStatus))
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", staleID).Scan(&staleStatus))
	require.Equal(t, string(StatusRetry), appliedStatus, "matching row must be applied atomically in the same statement")
	require.Equal(t, string(StatusLeased), staleStatus, "CAS-mismatched row must not be touched")
}

func TestPgxRepositoryRouteFailures_ExpiredLeaseRejected(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-presend-expired"
	id := insertAndClaimRoutingRow(t, repository, workerID, "room-presend-expired", "stream-presend-expired")

	_, err := pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	err = repository.RouteFailures(ctx, []FailureUpdate{
		{ID: id, AttemptCount: 1, Error: "expired", TargetStatus: StatusRetry},
	}, workerID)
	miss := requireFenceMiss(t, err, sendfence.ReasonLeaseExpired)
	require.Equal(t, []int64{id}, miss.UnappliedDeliveryIDs())

	var status string
	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, attempt_count FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &attemptCount))
	require.Equal(t, string(StatusLeased), status, "expired-lease row is owned by RecoverExpiredLeased, not RouteFailures")
	require.Equal(t, 0, attemptCount)
}

func TestPgxRepositoryRouteSendingFailures_AppliesToLeasedRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-postsend-leased"
	id := insertAndClaimRoutingRow(t, repository, workerID, "room-postsend-leased", "stream-postsend-leased")

	require.NoError(t, repository.RouteSendingFailures(ctx, []FailureUpdate{
		{ID: id, AttemptCount: 1, RetryAfter: time.Minute, Error: "mark sending failed", TargetStatus: StatusRetry},
	}, workerID))

	var status string
	var lockedBy *string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &lockedBy))
	require.Equal(t, string(StatusRetry), status, "post-send variant must also cover still-leased rows")
	require.Nil(t, lockedBy)
}

func TestPgxRepositoryRouteSendingFailures_QuarantinePreemptedRowNotOverwritten(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-quarantine-preempt"
	record := insertAndClaimRoutingRecord(t, repository, workerID, "room-quarantine-preempt", "stream-quarantine-preempt")
	id := record.ID
	beginSendingFixture(t, repository, workerID, record)

	_, err := pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET sending_started_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)
	quarantined, err := repository.QuarantineStaleSending(ctx, time.Second, 10)
	require.NoError(t, err)
	require.Equal(t, 1, quarantined)

	err = repository.RouteSendingFailures(ctx, []FailureUpdate{
		{ID: id, AttemptCount: 1, Error: "must not overwrite", TargetStatus: StatusRetry},
	}, workerID)
	miss := requireFenceMiss(t, err, sendfence.ReasonStatusChanged)
	require.Equal(t, []int64{id}, miss.UnappliedDeliveryIDs())

	var status, lastError, lastErrorCode string
	var lockedBy *string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, last_error, last_error_code, locked_by FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &lastError, &lastErrorCode, &lockedBy))
	require.Equal(t, string(StatusQuarantined), status, "quarantined row must stay quarantined")
	require.Equal(t, "stale sending; external send outcome unknown", lastError, "quarantine error must not be overwritten")
	require.Equal(t, "stale_sending", lastErrorCode, "maintenance quarantine must record its static code")
	require.Nil(t, lockedBy)
}

func TestPgxRepositoryRouteFailures_RejectsUnsupportedTargetStatus(t *testing.T) {
	repository, _ := setupDispatchOutboxIntegration(t)
	ctx := context.Background()

	err := repository.RouteFailures(ctx, []FailureUpdate{
		{ID: 1, AttemptCount: 1, TargetStatus: StatusSent},
	}, "worker-any")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported target status")
}

func TestPgxRepositoryRouteSendingFailures_ExpiredLeaseSendingToDLQRecordsTerminalFields(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-sending-dlq"
	record := insertAndClaimRoutingRecord(t, repository, workerID, "room-sending-dlq", "stream-sending-dlq")
	id := record.ID
	beginSendingFixture(t, repository, workerID, record)

	_, err := pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET attempt_count = 2,
			lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	var nextAttemptBefore time.Time
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT next_attempt_at FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&nextAttemptBefore))

	require.NoError(t, repository.RouteSendingFailures(ctx, []FailureUpdate{
		{ID: id, AttemptCount: 3, RetryAfter: time.Hour, Error: "sending retry exhausted", TargetStatus: StatusDLQ},
	}, workerID), "소유 worker의 sending→dlq 전이는 만료된 lease에서도 적용돼야 한다")

	var status, lastError string
	var attemptCount int
	var dlqAt *time.Time
	var lockedBy *string
	var nextAttemptAfter time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_error, attempt_count, dlq_at, locked_by, next_attempt_at
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&status, &lastError, &attemptCount, &dlqAt, &lockedBy, &nextAttemptAfter))
	require.Equal(t, string(StatusDLQ), status)
	require.Equal(t, "sending retry exhausted", lastError)
	require.Equal(t, 3, attemptCount, "DLQ terminal row must record the final attempt count")
	require.NotNil(t, dlqAt, "dlq_at must be recorded by the dlq CASE branch")
	require.Nil(t, lockedBy)
	require.True(t, nextAttemptAfter.Equal(nextAttemptBefore),
		"dlq branch must keep next_attempt_at untouched (input value applies to retry rows only)")
}

func TestPgxRepositoryRecoverExpiredLeased_RecordsStaticErrorCode(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	id := insertAndClaimRoutingRow(t, repository, "worker-lease-code", "room-lease-code", "stream-lease-code")

	_, err := pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET lock_expires_at = NOW() - INTERVAL '10 seconds'
		WHERE id = $1`, id)
	require.NoError(t, err)

	recovered, err := repository.RecoverExpiredLeased(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)

	var status, lastError, lastErrorCode string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status, last_error, last_error_code FROM alarm_dispatch_deliveries WHERE id=$1", id,
	).Scan(&status, &lastError, &lastErrorCode))
	require.Equal(t, string(StatusRetry), status)
	require.Equal(t, "lease expired before external send", lastError)
	require.Equal(t, "lease_expired", lastErrorCode)
}

func TestConsumerRouteFailures_ChokepointTruncatesSanitizesAndRecordsCode(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-chokepoint"
	id := insertAndClaimRoutingRow(t, repository, workerID, "room-chokepoint", "stream-chokepoint")

	consumer := mustNewConsumer(t, repository, nil, nil, WithWorkerID(workerID))
	envelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: id,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  int64(time.Minute / time.Millisecond),
			LastError:     "Bearer secret-token-value " + strings.Repeat("한", 900),
			LastErrorCode: ErrorCodeHTTP5xx,
		},
	}
	require.NoError(t, consumer.RouteFailures(ctx, []domain.AlarmQueueEnvelope{envelope}, nil))

	var status, lastError, lastErrorCode string
	var errorBytes int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, last_error, last_error_code, octet_length(last_error)
		FROM alarm_dispatch_deliveries WHERE id=$1`, id,
	).Scan(&status, &lastError, &lastErrorCode, &errorBytes))
	require.Equal(t, string(StatusRetry), status)
	require.Equal(t, ErrorCodeHTTP5xx, lastErrorCode)
	require.LessOrEqual(t, errorBytes, 2048, "sanitized last_error must fit the byte budget")
	require.NotContains(t, lastError, "secret-token-value", "bearer token must be redacted before persistence")
	require.Contains(t, lastError, "[redacted]")
}
