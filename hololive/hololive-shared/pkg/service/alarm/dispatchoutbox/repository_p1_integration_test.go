//go:build integration

package dispatchoutbox

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertClaimedEnvelope(t *testing.T, repository *PgxRepository, workerID string) *Record {
	t.Helper()
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-dlq-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-dlq-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, workerID, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)
	return records[0]
}

func TestPgxRepositoryMoveToDLQ_AcceptsSendingRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	workerID := "worker-dlq"

	record := insertClaimedEnvelope(t, repository, workerID)
	require.NoError(t, repository.MarkSending(ctx, []int64{record.ID}, workerID, time.Minute))

	err := repository.MoveToDLQ(ctx, []TerminalUpdate{{ID: record.ID, Error: "sending retry exhausted"}}, workerID)

	require.NoError(t, err,
		"persistSendingRetry/MarkSending 보상의 attempt 소진 분기는 'sending' 행을 DLQ로 옮길 수 있어야 한다")
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", record.ID,
	).Scan(&status))
	assert.Equal(t, string(StatusDLQ), status)
}

func TestPgxRepositoryMoveToDLQ_StillRejectsForeignWorkerSendingRows(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()

	record := insertClaimedEnvelope(t, repository, "worker-owner")
	require.NoError(t, repository.MarkSending(ctx, []int64{record.ID}, "worker-owner", time.Minute))

	err := repository.MoveToDLQ(ctx, []TerminalUpdate{{ID: record.ID, Error: "foreign"}}, "worker-foreign")

	require.Error(t, err)
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", record.ID,
	).Scan(&status))
	assert.Equal(t, string(StatusSending), status)
}

func TestLoadExistingEventRows_ReturnsRowsInEventKeyOrder(t *testing.T) {
	repository, pool := setupDispatchOutboxIntegration(t)
	ctx := context.Background()
	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)

	envelopes := make([]domain.AlarmQueueEnvelope, 0, 4)
	for _, streamID := range []string{"stream-d", "stream-a", "stream-c", "stream-b"} {
		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    "room-order",
				Channel:   &domain.Channel{ID: "channel-1"},
				Stream:    &domain.Stream{ID: streamID, ChannelID: "channel-1", StartScheduled: &start},
			},
			Version: 1,
		})
	}
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes, Status: StatusPending})
	require.NoError(t, err)

	events := make([]eventInsert, 0, len(envelopes))
	for i := range envelopes {
		event, _, buildErr := buildLedgerRows(&envelopes[i], StatusPending)
		require.NoError(t, buildErr)
		events = append(events, event)
	}

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, mustSQL("repository_event_preflight_0033_01.sql"), eventKeysOf(events))
	require.NoError(t, err)
	defer rows.Close()

	got := make([]string, 0, len(events))
	for rows.Next() {
		var id int64
		var eventKey, payloadHash string
		require.NoError(t, rows.Scan(&id, &eventKey, &payloadHash))
		got = append(got, eventKey)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, len(events))
	assert.IsIncreasing(t, got,
		"FOR UPDATE의 잠금 획득 순서가 event_key 정렬로 고정되어야 복수 publisher 교차 잠금(40P01)이 없다")
}

func eventKeysOf(events []eventInsert) []string {
	keys := make([]string, 0, len(events))
	for i := range events {
		keys = append(keys, events[i].EventKey)
	}
	return keys
}
