package notificationdelivery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestOutboxSettlementCannotOverwriteConcurrentQuarantine(t *testing.T) {
	for _, test := range []struct {
		name   string
		action string
		settle func(context.Context, *outboxFixture, int64) error
	}{
		{name: "sent", action: actionMarkSent, settle: func(ctx context.Context, repo *outboxFixture, id int64) error {
			return repo.MarkSent(ctx, id, testWorkerA, 0, testIrisRequestID, testNotificationHandoffHorizon)
		}},
		{name: "failed", action: actionMarkFailed, settle: func(ctx context.Context, repo *outboxFixture, id int64) error {
			return repo.MarkFailed(ctx, id, testWorkerA, 0, 3, time.Second, "late known failure")
		}},
		{name: "quarantined", action: actionMarkQuarantined, settle: func(ctx context.Context, repo *outboxFixture, id int64) error {
			return repo.MarkQuarantined(ctx, id, testWorkerA, 0, "late unknown outcome")
		}},
		{name: "reissue", action: actionReissueRequest, settle: func(ctx context.Context, repo *outboxFixture, id int64) error {
			return repo.reissueFailedRequest(ctx, id, testWorkerA, 0, 0, false, 3, time.Second, "late pre-handoff conflict")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "settlement-period", "settlement-room", "message"))

			rows := fetchAndLockItems(ctx, t, repo)
			require.Len(t, rows, 1)

			id := rows[0].ID
			request := &deliverypayload.Request{Body: "settlement body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&rows[0])}
			require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, rows[0].AttemptCount, request))
			markOutboxSending(ctx, t, repo, &rows[0])

			tx, err := repo.pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackTestTx(ctx, t, tx)

			_, err = tx.Exec(ctx, `UPDATE notification_delivery_outbox SET sending_started_at=clock_timestamp()-INTERVAL '10 minutes' WHERE id=$1`, id)
			require.NoError(t, err)

			tag, err := tx.Exec(ctx, mustSQL("outbox_repository_0301_10.sql"), float64(time.Minute.Milliseconds()), 1, "stale sending")
			require.NoError(t, err)
			require.EqualValues(t, 1, tag.RowsAffected())

			done := make(chan error, 1)

			go func() { done <- test.settle(ctx, repo, id) }()

			waitForOutboxQueryLock(ctx, t, repo, tx)
			require.NoError(t, tx.Commit(ctx))

			requireFenceMiss(t, <-done, test.action, sendfence.ReasonStatusChanged)

			var (
				status     string
				attempts   int
				errText    string
				generation int
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count,error,(payload->'request'->>'generation')::int
				FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &attempts, &errText, &generation))
			require.Equal(t, "QUARANTINED", status)
			require.Zero(t, attempts)
			require.Equal(t, "stale sending", errText)
			require.Zero(t, generation)
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

	done := make(chan error, 1)

	go func() { done <- repo.MarkSending(ctx, id, testWorkerA, rows[0].AttemptCount, testLease) }()

	waitForOutboxQueryLock(ctx, t, repo, tx)
	require.NoError(t, tx.Commit(ctx))

	requireFenceMiss(t, <-done, actionMarkSending, sendfence.ReasonOwnerChanged)

	var status, owner string

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,locked_by FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &owner))
	require.Equal(t, "PENDING", status)
	require.Equal(t, testWorkerB, owner)
}

// lease를 넘겨 멈췄던 이전 소유자의 늦은 전이는 다른 worker가 재claim해 발송 중인 행을 바꾸지 않는다.
func TestStaleOwnerTransitionsLeaveReclaimedSendingRowUntouched(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "stale-owner", "stale-owner-room", "message"))

	stale := fetchAndLockItems(ctx, t, repo)[0]
	request := &deliverypayload.Request{Body: "stale owner body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&stale)}
	require.NoError(t, repo.saveRequest(ctx, stale.ID, testWorkerA, stale.AttemptCount, request))

	expireLease(ctx, t, repo, stale.ID)
	requireFenceMiss(t, repo.MarkSending(ctx, stale.ID, testWorkerA, stale.AttemptCount, testLease), actionMarkSending, sendfence.ReasonLeaseExpired)
	reclaimByWorkerB(ctx, t, repo, stale.ID)
	require.NoError(t, repo.MarkSending(ctx, stale.ID, testWorkerB, stale.AttemptCount, testLease))

	before := readOwnerRow(ctx, t, repo, stale.ID)

	for _, transition := range []struct {
		action string
		err    error
	}{
		{actionMarkSending, repo.MarkSending(ctx, stale.ID, testWorkerA, stale.AttemptCount, testLease)},
		{actionMarkSent, repo.MarkSent(ctx, stale.ID, testWorkerA, stale.AttemptCount, testIrisRequestID, testNotificationHandoffHorizon)},
		{actionMarkFailed, repo.MarkFailed(ctx, stale.ID, testWorkerA, stale.AttemptCount, 3, time.Second, "stale failure")},
		{actionMarkQuarantined, repo.MarkQuarantined(ctx, stale.ID, testWorkerA, stale.AttemptCount, "stale unknown")},
		{actionReissueRequest, repo.reissueFailedRequest(ctx, stale.ID, testWorkerA, stale.AttemptCount, 0, false, 3, time.Second, "stale conflict")},
	} {
		wantReason := sendfence.ReasonOwnerChanged

		if transition.action == actionMarkSending {
			wantReason = sendfence.ReasonStatusChanged
		}

		requireFenceMiss(t, transition.err, transition.action, wantReason)
	}

	require.Equal(t, before, readOwnerRow(ctx, t, repo, stale.ID))
	require.Zero(t, storedRequest(ctx, t, repo, stale.ID).Generation)
	require.NoError(t, repo.MarkSent(ctx, stale.ID, testWorkerB, stale.AttemptCount, testIrisRequestID, testNotificationHandoffHorizon))
}
