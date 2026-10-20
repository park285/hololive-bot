package notificationdelivery

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestYieldBetweenCleanupBatchesReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := yieldBetweenCleanupBatches(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("yieldBetweenCleanupBatches() error = %v, want context.Canceled", err)
	}
}

type cleanupRearmCase struct {
	name      string
	request   bool
	exhausted bool
}

func TestCleanupConcurrentProducerRearm(t *testing.T) {
	// producer API는 pool 전용이므로 원본 SQL을 트랜잭션에서 실행해 commit 경계를 제어한다.
	upsertSQL, err := os.ReadFile("../../../../hololive-shared/pkg/service/delivery/queries/outbox_enqueue_batch_upsert.sql")
	require.NoError(t, err)

	for _, test := range []cleanupRearmCase{
		{name: "ordinary_failed"},
		{name: "immutable_request", request: true},
		{name: "exhausted_request", request: true, exhausted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			item, originalPayload := seedCleanupRearm(ctx, t, repo, test)

			tx, err := repo.pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackTestTx(t.Context(), t, tx)

			tag, err := tx.Exec(ctx, string(upsertSQL), `[{"kind":"MEMBER_NEWS_WEEKLY","period_key":"cleanup-rearm","room_id":"cleanup-room","content_id":"cleanup-rearm:cleanup-room","payload":{"message":"replacement message"}}]`)
			require.NoError(t, err)

			wantPayload, wantDeleted := assertCleanupRearmed(ctx, t, test, originalPayload, tag.RowsAffected(), tx, item.ID)

			type result struct {
				deleted int64
				err     error
			}

			done := make(chan result, 1)

			go func() {
				deleted, err := repo.Cleanup(ctx, 30*24*time.Hour)
				done <- result{deleted, err}
			}()

			// 잠금 대기를 관측한 뒤에만 commit해 cleanup의 오래된 후보 snapshot을 보장한다.
			waitForOutboxQueryLock(ctx, t, repo, tx)
			require.NoError(t, tx.Commit(ctx))

			got := <-done
			require.NoError(t, got.err)
			require.Equal(t, wantDeleted, got.deleted)

			if test.exhausted {
				var remaining int

				require.NoError(t, repo.pool.QueryRow(ctx, `SELECT count(*) FROM notification_delivery_outbox WHERE id=$1`, item.ID).Scan(&remaining))
				require.Zero(t, remaining, "terminal exhausted requests remain eligible for retention")

				return
			}

			var (
				status         string
				attempts       int
				payloadMatches bool
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count,payload=$2::jsonb FROM notification_delivery_outbox WHERE id=$1`, item.ID, wantPayload).Scan(&status, &attempts, &payloadMatches))
			require.Equal(t, "PENDING", status)
			require.Zero(t, attempts)
			require.True(t, payloadMatches, "cleanup must preserve the producer payload and immutable request")
		})
	}
}

func assertCleanupRearmed(ctx context.Context, t *testing.T, test cleanupRearmCase, originalPayload string, affectedRows int64, tx pgx.Tx, id int64) (string, int64) {
	t.Helper()

	wantStatus := "PENDING"
	wantPayload := `{"message":"replacement message"}`
	wantAttempts := 0

	var wantDeleted int64

	if test.request {
		wantPayload = originalPayload
	}

	if test.exhausted {
		wantStatus = "FAILED"
		wantAttempts = 1
		wantDeleted = 1

		require.Zero(t, affectedRows, "exhausted requests must not rearm")
	} else {
		require.EqualValues(t, 1, affectedRows)
	}

	var (
		status         string
		attempts       int
		payloadMatches bool
	)

	require.NoError(t, tx.QueryRow(ctx, `SELECT status,attempt_count,payload=$2::jsonb FROM notification_delivery_outbox WHERE id=$1`, id, wantPayload).Scan(&status, &attempts, &payloadMatches))
	require.Equal(t, wantStatus, status)
	require.Equal(t, wantAttempts, attempts)
	require.True(t, payloadMatches)

	return wantPayload, wantDeleted
}

func seedCleanupRearm(ctx context.Context, t *testing.T, repo *outboxFixture, test cleanupRearmCase) (*domain.NotificationDeliveryOutbox, string) {
	t.Helper()

	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "cleanup-rearm", "cleanup-room", "original message"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	item := &items[0]

	if test.request {
		request := &deliverypayload.Request{
			Body:      "immutable body",
			Route:     testPreparedText,
			BaseID:    notificationDeliveryClientRequestID(item),
			Exhausted: test.exhausted,
		}
		require.NoError(t, repo.saveRequest(ctx, item.ID, testWorkerA, item.AttemptCount, request))
	}

	require.NoError(t, repo.MarkFailed(ctx, item.ID, testWorkerA, item.AttemptCount, 1, time.Second, "known failure"))

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET created_at=clock_timestamp()-INTERVAL '90 days' WHERE id=$1`, item.ID)
	require.NoError(t, err)

	var originalPayload string

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT payload::text FROM notification_delivery_outbox WHERE id=$1`, item.ID).Scan(&originalPayload))

	return item, originalPayload
}

func TestCleanupConcurrentTerminalTimestampRefresh(t *testing.T) {
	for _, test := range []struct {
		name   string
		status domain.DeliveryOutboxStatus
	}{
		{name: "created_at", status: domain.DeliveryStatusFailed},
		{name: "sent_at", status: domain.DeliveryStatusSent},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "cleanup-cutoff", "cleanup-room", "message"))

			var id int64

			err := repo.pool.QueryRow(ctx, `
				UPDATE notification_delivery_outbox
				SET status=$1, created_at=clock_timestamp()-INTERVAL '90 days',
					sent_at=CASE WHEN $1='SENT' THEN clock_timestamp()-INTERVAL '90 days' ELSE NULL END
				RETURNING id
			`, test.status).Scan(&id)
			require.NoError(t, err)

			tx, err := repo.pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackTestTx(t.Context(), t, tx)

			_, err = tx.Exec(ctx, `
				UPDATE notification_delivery_outbox
				SET created_at=CASE WHEN status='FAILED' THEN clock_timestamp() ELSE created_at END,
					sent_at=CASE WHEN status='SENT' THEN clock_timestamp() ELSE sent_at END
				WHERE id=$1
			`, id)
			require.NoError(t, err)

			type result struct {
				deleted int64
				err     error
			}

			done := make(chan result, 1)

			go func() {
				deleted, cleanupErr := repo.Cleanup(ctx, 30*24*time.Hour)
				done <- result{deleted, cleanupErr}
			}()

			waitForOutboxQueryLock(ctx, t, repo, tx)
			require.NoError(t, tx.Commit(ctx))

			got := <-done
			require.NoError(t, got.err)
			require.Zero(t, got.deleted, "a still-terminal row with a refreshed retention timestamp must survive")

			var (
				status domain.DeliveryOutboxStatus
				recent bool
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,COALESCE(sent_at,created_at)>clock_timestamp()-INTERVAL '1 day' FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &recent))
			require.Equal(t, test.status, status)
			require.True(t, recent)

			_, err = repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox
				SET created_at=clock_timestamp()-INTERVAL '31 days',
					sent_at=CASE WHEN status='SENT' THEN clock_timestamp()-INTERVAL '31 days' ELSE NULL END
				WHERE id=$1`, id)
			require.NoError(t, err)

			deleted, err := repo.Cleanup(ctx, 30*24*time.Hour)
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted, "terminal rows must still be removed once the cutoff passes")
		})
	}
}

// 보존 기간 경계의 직전·직후에 둔 종료 행 중 기한을 넘긴 행만 지운다.
func TestCleanupRetentionBoundaryDeletesOnlyExpiredRows(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()

	for _, room := range []string{"expired-room", "retained-room"} {
		require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "retention-boundary", room, "message"))
	}

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET status='FAILED',
		created_at=clock_timestamp()-CASE WHEN room_id='expired-room' THEN INTERVAL '7 days 1 minute' ELSE INTERVAL '7 days' - INTERVAL '1 minute' END`)
	require.NoError(t, err)

	deleted, err := repo.Cleanup(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining string

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT room_id FROM notification_delivery_outbox`).Scan(&remaining))
	require.Equal(t, "retained-room", remaining)
}
