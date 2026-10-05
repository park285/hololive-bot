package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestOutboxSettlementCannotOverwriteConcurrentQuarantine(t *testing.T) {
	for _, test := range []struct {
		name   string
		settle func(context.Context, *OutboxRepository, int64) (bool, error)
	}{
		{name: "sent", settle: func(ctx context.Context, repo *OutboxRepository, id int64) (bool, error) {
			return repo.MarkSent(ctx, id, testWorkerA)
		}},
		{name: "failed", settle: func(ctx context.Context, repo *OutboxRepository, id int64) (bool, error) {
			return repo.MarkFailed(ctx, id, testWorkerA, 0, 3, time.Second, "late known failure")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "settlement-period", "settlement-room", "message"))

			rows := fetchAndLockItems(ctx, t, repo)
			require.Len(t, rows, 1)

			id := rows[0].ID
			ok, err := repo.MarkSending(ctx, id, testWorkerA, testLease)
			require.NoError(t, err)
			require.True(t, ok)

			tx, err := repo.pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackTestTx(ctx, t, tx)

			_, err = tx.Exec(ctx, `UPDATE notification_delivery_outbox SET sending_started_at=clock_timestamp()-INTERVAL '10 minutes' WHERE id=$1`, id)
			require.NoError(t, err)

			tag, err := tx.Exec(ctx, mustSQL("outbox_repository_0301_10.sql"), deliveryStatusSending, float64(time.Minute.Milliseconds()), 1, deliveryStatusQuarantined, "stale sending")
			require.NoError(t, err)
			require.EqualValues(t, 1, tag.RowsAffected())

			type result struct {
				ok  bool
				err error
			}

			done := make(chan result, 1)

			go func() { ok, err := test.settle(ctx, repo, id); done <- result{ok, err} }()

			waitForOutboxQueryLock(ctx, t, repo, tx)
			require.NoError(t, tx.Commit(ctx))

			got := <-done
			require.NoError(t, got.err)
			require.False(t, got.ok)

			var (
				status   string
				attempts int
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &attempts))
			require.Equal(t, "QUARANTINED", status)
			require.Zero(t, attempts)
		})
	}
}

func TestMarkSendingCannotUsePreviousOwnerAfterConcurrentReclaim(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reclaim-period", "reclaim-room", "message"))

	rows := fetchAndLockItems(ctx, t, repo)
	require.Len(t, rows, 1)

	id := rows[0].ID
	tx, err := repo.pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackTestTx(ctx, t, tx)

	// fetch-and-lock가 새 소유권을 커밋하는 동안 이전 소유자가 발송 전이에 진입한다.
	_, err = tx.Exec(ctx, `UPDATE notification_delivery_outbox SET locked_by=$2,locked_at=clock_timestamp(),lock_expires_at=clock_timestamp()+INTERVAL '1 minute' WHERE id=$1`, id, testWorkerB)
	require.NoError(t, err)

	type result struct {
		ok  bool
		err error
	}

	done := make(chan result, 1)

	go func() { ok, err := repo.MarkSending(ctx, id, testWorkerA, testLease); done <- result{ok, err} }()

	waitForOutboxQueryLock(ctx, t, repo, tx)
	require.NoError(t, tx.Commit(ctx))

	got := <-done
	require.NoError(t, got.err)
	require.False(t, got.ok)

	var status, owner string

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,locked_by FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &owner))
	require.Equal(t, "PENDING", status)
	require.Equal(t, testWorkerB, owner)
}
