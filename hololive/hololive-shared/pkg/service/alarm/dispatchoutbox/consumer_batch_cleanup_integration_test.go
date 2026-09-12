//go:build integration

package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type batchCleanupPgxRepository struct {
	*PgxRepository

	load         func(context.Context, []int64) (map[int64]EventRecord, error)
	terminalErr  error
	releaseCalls int
}

func (r *batchCleanupPgxRepository) LoadEventsByID(ctx context.Context, ids []int64) (map[int64]EventRecord, error) {
	return r.load(ctx, ids)
}

func (r *batchCleanupPgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, owner string) error {
	if r.terminalErr != nil {
		return r.terminalErr
	}

	return r.PgxRepository.MoveToDLQ(ctx, updates, owner)
}

func (r *batchCleanupPgxRepository) ReleaseLeased(ctx context.Context, ids []int64, owner string) error {
	r.releaseCalls++

	return r.PgxRepository.ReleaseLeased(ctx, ids, owner)
}

type batchCleanupIntegrationSnapshot struct {
	id, eventID, unitID int64
	keys                []string
}

func prepareBatchCleanupIntegration(
	ctx context.Context,
	t *testing.T,
	repository *PgxRepository,
	pool *pgxpool.Pool,
) []batchCleanupIntegrationSnapshot {
	t.Helper()

	start := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	envelopes := make([]domain.AlarmQueueEnvelope, 0, 3)

	for i := range 3 {
		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    testRoomID,
				Channel:   &domain.Channel{ID: testChannelID},
				Stream:    &domain.Stream{ID: fmt.Sprintf("cleanup-stream-%d", i), ChannelID: testChannelID, StartScheduled: &start},
			},
			ClaimKeys: []string{fmt.Sprintf("notified:claim:cleanup-%d", i)},
			Version:   1,
		})
	}

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: envelopes, Status: StatusPending})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "UPDATE alarm_dispatch_deliveries SET attempt_count=4")
	require.NoError(t, err)

	rows, err := pool.Query(ctx, "SELECT id,event_id,send_unit_id,claim_keys FROM alarm_dispatch_deliveries ORDER BY id")
	require.NoError(t, err)

	before := make([]batchCleanupIntegrationSnapshot, 0, 3)

	for rows.Next() {
		var row batchCleanupIntegrationSnapshot

		require.NoError(t, rows.Scan(&row.id, &row.eventID, &row.unitID, &row.keys))

		before = append(before, row)
	}

	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, before, 3)

	for _, row := range before {
		require.Positive(t, row.unitID)
	}

	return before
}

func newBatchCleanupIntegrationConsumer(
	t *testing.T,
	mode string,
	repository *PgxRepository,
	pool *pgxpool.Pool,
	before []batchCleanupIntegrationSnapshot,
	sentinel error,
) (*Consumer, *batchCleanupPgxRepository, *fakeClaimKeyReleaser) {
	t.Helper()

	wrapper := &batchCleanupPgxRepository{PgxRepository: repository}

	wrapper.load = func(ctx context.Context, ids []int64) (map[int64]EventRecord, error) {
		if mode == "other owner fence" {
			_, err := pool.Exec(ctx, "UPDATE alarm_dispatch_deliveries SET locked_by='other-owner' WHERE id=$1", before[2].id)
			require.NoError(t, err)

			return nil, sentinel
		}

		if mode == "load failure" {
			return nil, sentinel
		}

		events, err := repository.LoadEventsByID(ctx, ids)
		require.NoError(t, err)

		event := events[before[1].eventID]

		event.Payload = []byte("{")

		events[before[1].eventID] = event

		return events, nil
	}

	releaser := &fakeClaimKeyReleaser{}

	if mode == "terminal failure" {
		wrapper.terminalErr = sentinel
	}

	if mode == "terminal key failure" {
		releaser.err = sentinel
	}

	consumer := NewConsumer(wrapper, nil, WithWorkerID("batch-owner"), WithClaimKeyReleaser(releaser))

	return consumer, wrapper, releaser
}

func assertBatchCleanupIntegrationState(
	ctx context.Context,
	t *testing.T,
	mode string,
	repository *PgxRepository,
	pool *pgxpool.Pool,
	before []batchCleanupIntegrationSnapshot,
	wrapper *batchCleanupPgxRepository,
	releaser *fakeClaimKeyReleaser,
) {
	t.Helper()

	retryIDs := make([]int64, 0, 3)

	for i, beforeRow := range before {
		var status string

		var attempt int

		var unitID int64

		var keys []string

		var owner *string

		err := pool.QueryRow(ctx, "SELECT status,attempt_count,send_unit_id,claim_keys,locked_by FROM alarm_dispatch_deliveries WHERE id=$1", beforeRow.id).Scan(&status, &attempt, &unitID, &keys, &owner)
		require.NoError(t, err)
		require.Equal(t, 4, attempt)
		require.Equal(t, beforeRow.unitID, unitID)
		require.Equal(t, beforeRow.keys, keys)

		switch {
		case mode == "terminal key failure" && i == 1:
			require.Equal(t, string(StatusDLQ), status)
			require.Nil(t, owner)
		case mode == "other owner fence" && i == 2:
			require.Equal(t, string(StatusLeased), status)
			require.Equal(t, new("other-owner"), owner)
		default:
			require.Equal(t, string(StatusRetry), status)
			require.Nil(t, owner)

			retryIDs = append(retryIDs, beforeRow.id)
		}
	}

	if mode == "terminal key failure" {
		require.Equal(t, 1, releaser.calls)
		require.Equal(t, before[1].keys, releaser.lastKeys)
	} else {
		require.Zero(t, releaser.calls)
	}

	reclaimed, err := repository.ClaimDue(ctx, "next-owner", 3, time.Minute)
	require.NoError(t, err)

	reclaimedIDs := make([]int64, 0, len(reclaimed))
	for _, row := range reclaimed {
		reclaimedIDs = append(reclaimedIDs, row.ID)
		require.Equal(t, 4, row.AttemptCount)
	}

	require.ElementsMatch(t, retryIDs, reclaimedIDs)
	require.Equal(t, 1, wrapper.releaseCalls)
}

func TestConsumerFailedBatchCleanupIntegration(t *testing.T) {
	for _, mode := range []string{"load failure", "terminal failure", "terminal key failure", "other owner fence"} {
		t.Run(mode, func(t *testing.T) {
			repository, pool := setupDispatchOutboxIntegration(t)
			ctx := t.Context()
			before := prepareBatchCleanupIntegration(ctx, t, repository, pool)

			sentinel := errors.New("injected batch failure")
			consumer, wrapper, releaser := newBatchCleanupIntegrationConsumer(t, mode, repository, pool, before, sentinel)

			out, err := consumer.DrainBatch(ctx, 3)
			require.Nil(t, out)
			require.ErrorIs(t, err, sentinel)

			if mode == "other owner fence" {
				require.Contains(t, err.Error(), "release failed outbox batch")
			}

			assertBatchCleanupIntegrationState(ctx, t, mode, repository, pool, before, wrapper, releaser)
		})
	}
}
