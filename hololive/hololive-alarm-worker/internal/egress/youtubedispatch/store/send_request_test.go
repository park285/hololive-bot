package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testFrozenTextRoute = "text"

func TestFrozenRequestPreservesBodyRouteGenerationAndMembership(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedTransitionLogicalGroup(t, pool, "freeze-video", "freeze-room")

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	frozen, err := transition.FreezeRequest(ctx, prepared.ActiveRows, FrozenRequest{BaseID: "youtube:freeze", RoomID: "freeze-room", Message: "original body", Route: "markdown", DedupeKeys: []string{"key"}})
	require.NoError(t, err)

	operation, _, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)

	advanced, err := transition.AdvanceRequestGeneration(ctx, operation, frozen)
	require.NoError(t, err)
	require.Equal(t, 1, advanced.Generation)

	_, err = transition.AdvanceRequestGeneration(ctx, operation, frozen)
	require.Error(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=now()-interval '1 second'`)
	require.NoError(t, err)

	claimed, err = transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	prepared, err = transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	restored, err := transition.FreezeRequest(ctx, prepared.ActiveRows, FrozenRequest{BaseID: "different", RoomID: "freeze-room", Message: "changed template", Route: testFrozenTextRoute, DedupeKeys: []string{"changed"}})
	require.NoError(t, err)
	require.Equal(t, "original body", restored.Message)
	require.Equal(t, "markdown", restored.Route)
	require.Equal(t, 1, restored.Generation)

	id, err := restored.ClientRequestID()
	require.NoError(t, err)
	require.Equal(t, "youtube:freeze:r1", id)
}

func TestExpiredPendingConvergesWithoutLedgerAndCannotRevive(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	older, newer := seedTransitionLogicalGroup(t, pool, "expired-video", "expired-room")
	_, err := pool.Exec(ctx, `UPDATE youtube_notification_outbox SET created_at=now()-interval '10 days'`)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, claimed)

	expired, err := transition.ExpirePending(ctx, 0, 10)
	require.NoError(t, err)
	require.Equal(t, 2, expired.Expired)

	for _, id := range []int64{older, newer} {
		var (
			status domain.OutboxStatus
			reason string
		)

		require.NoError(t, pool.QueryRow(ctx, `SELECT status,error FROM youtube_notification_delivery WHERE id=$1`, id).Scan(&status, &reason))
		require.Equal(t, domain.OutboxStatusFailed, status)
		require.Equal(t, expiredPendingReason, reason)
	}

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_notification_delivery_ledger`).Scan(&count))
	require.Zero(t, count)

	again, err := transition.ExpirePending(ctx, 0, 10)
	require.NoError(t, err)
	require.Zero(t, again.Expired)

	revived, err := transition.ReviveFailedLogicalGroups(ctx, 30*24*time.Hour, 10)
	require.NoError(t, err)
	require.Zero(t, revived.RevivedDeliveries)

	repository := NewDeliveryRepository(pool, nil)
	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, expired.TouchedOutboxIDs))

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_outbox SET terminal_at=now()-interval '10 days'`)
	require.NoError(t, err)

	cleaned, err := transition.CleanupTerminalOutboxes(ctx, time.Now().Add(-time.Hour), CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 2, cleaned.DeletedOutboxes)
}

func TestExpiredPendingPreservesInFlightLogicalSibling(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	older, newer := seedTransitionLogicalGroup(t, pool, "expiry-inflight", "expiry-room")
	_, err := pool.Exec(ctx, `UPDATE youtube_notification_outbox SET created_at=now()-interval '10 days'`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET status='SENDING',locked_at=now() WHERE id=$1`, older)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)
	expired, err := transition.ExpirePending(ctx, 0, 10)
	require.NoError(t, err)
	require.Zero(t, expired.Expired)

	var status string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM youtube_notification_delivery WHERE id=$1`, newer).Scan(&status))
	require.Equal(t, "PENDING", status)
}

func TestFrozenGroupedClaimIsAtomicUnderContentionAndRespectsBatchBudget(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	first, duplicate := seedTransitionLogicalGroup(t, pool, "grouped-first", "grouped-room")
	_, err := pool.Exec(ctx, `DELETE FROM youtube_notification_delivery WHERE id=$1`, duplicate)
	require.NoError(t, err)

	second, duplicate := seedTransitionLogicalGroup(t, pool, "grouped-second", "grouped-room")

	_, err = pool.Exec(ctx, `DELETE FROM youtube_notification_delivery WHERE id=$1`, duplicate)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	_, err = transition.FreezeRequest(ctx, claimed, FrozenRequest{BaseID: "youtube:grouped", RoomID: "grouped-room", Message: "both", Route: testFrozenTextRoute, DedupeKeys: []string{"first", "second"}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=NULL`)
	require.NoError(t, err)

	assertConcurrentFrozenClaims(t, transition, first, second)

	third, duplicate := seedTransitionLogicalGroup(t, pool, "grouped-third", "grouped-room")

	_, err = pool.Exec(ctx, `DELETE FROM youtube_notification_delivery WHERE id=$1`, duplicate)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=NULL`)
	require.NoError(t, err)

	singleton, err := transition.ClaimPending(ctx, 1)
	require.NoError(t, err)
	require.Len(t, singleton, 1)
	require.Equal(t, third, singleton[0].ID, "oversized frozen group must not starve later eligible singleton")
}

func TestFrozenRequestReissueCommitLossKeepsReplayablePending(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedTransitionLogicalGroup(t, pool, "reissue-crash", "reissue-room")

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	frozen, err := transition.FreezeRequest(ctx, prepared.ActiveRows, FrozenRequest{BaseID: "youtube:crash", RoomID: "reissue-room", Message: "fixed", Route: testFrozenTextRoute, DedupeKeys: []string{"key"}})
	require.NoError(t, err)

	operation, _, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)

	transition.afterCommit = func(name string) error {
		if name == "advance request generation" {
			return errors.New("response lost")
		}

		return nil
	}
	_, err = transition.AdvanceRequestGeneration(ctx, operation, frozen)
	require.Error(t, err)

	var (
		status string
		count  int
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT status,attempt_count FROM youtube_notification_delivery WHERE id=$1`, prepared.ActiveRows[0].ID).Scan(&status, &count))
	require.Equal(t, "PENDING", status)
	require.Equal(t, 1, count)

	restored, err := transition.LoadFrozenRequests(ctx, []int64{prepared.ActiveRows[0].ID})
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(t, 1, restored[0].Generation)
	require.Equal(t, "fixed", restored[0].Message)
}

func TestFrozenRequestRetentionKeepsReferencedBodies(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedTransitionLogicalGroup(t, pool, "retention-request", "retention-room")

	transition := newTestTransitionStore(t, pool)
	claimed, err := transition.ClaimPending(ctx, 1)
	require.NoError(t, err)

	_, err = transition.FreezeRequest(ctx, claimed, FrozenRequest{BaseID: "youtube:retention", RoomID: "retention-room", Message: "body", Route: testFrozenTextRoute, DedupeKeys: []string{"key"}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET created_at=now()-interval '10 days'`)
	require.NoError(t, err)

	deleted, err := transition.CleanupOrphanRequests(ctx, time.Now().Add(-24*time.Hour), 10)
	require.NoError(t, err)
	require.Zero(t, deleted)

	_, err = pool.Exec(ctx, `DELETE FROM youtube_notification_delivery WHERE id=$1`, claimed[0].ID)
	require.NoError(t, err)

	deleted, err = transition.CleanupOrphanRequests(ctx, time.Now().Add(-24*time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
}

func assertConcurrentFrozenClaims(t *testing.T, transition *TransitionStore, first, second int64) {
	t.Helper()

	ctx := t.Context()

	var wg sync.WaitGroup

	results := make(chan []domain.YouTubeNotificationDelivery, 2)
	failures := make(chan error, 2)
	start := make(chan struct{})

	for range 2 {
		wg.Go(func() {
			<-start

			rows, claimErr := transition.ClaimPending(ctx, 2)
			results <- rows

			failures <- claimErr
		})
	}

	close(start)
	wg.Wait()
	close(results)
	close(failures)

	for claimErr := range failures {
		require.NoError(t, claimErr)
	}

	claimedCount := 0

	for rows := range results {
		if len(rows) == 0 {
			continue
		}

		require.Len(t, rows, 2)
		require.ElementsMatch(t, []int64{first, second}, []int64{rows[0].ID, rows[1].ID})

		claimedCount += len(rows)
	}

	require.Equal(t, 2, claimedCount)
}
