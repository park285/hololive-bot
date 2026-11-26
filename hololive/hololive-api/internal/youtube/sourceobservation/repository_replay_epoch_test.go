package sourceobservation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestReplayEpochActivationIsImmutableAndIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)

	first, err := repo.ActivateReplayEpoch(ctx, ReplayEpochInput{
		ActivatedBy: "operator-a",
		Reason:      "establish bounded historical replay",
	})
	if err != nil {
		t.Fatalf("activate replay epoch: %v", err)
	}

	if !first.Activated || first.Epoch.CutoffReceivedAt.IsZero() {
		t.Fatalf("first activation = %#v", first)
	}

	second, err := repo.ActivateReplayEpoch(ctx, ReplayEpochInput{
		ActivatedBy: "operator-b",
		Reason:      "must not replace original epoch",
	})
	if err != nil {
		t.Fatalf("repeat replay epoch activation: %v", err)
	}

	if second.Activated || second.Epoch != first.Epoch {
		t.Fatalf("second activation = %#v, want original %#v", second, first.Epoch)
	}

	assertTableCount(t, pool, "source_observation_replay_epoch", 1)
}

func TestReplayEpochActivationWaitsForConcurrentInsert(t *testing.T) {
	t.Parallel()

	for _, commit := range []bool{true, false} {
		name := "commit"

		if !commit {
			name = "rollback"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			testReplayEpochConcurrentActivation(t, commit)
		})
	}
}

func testReplayEpochConcurrentActivation(t *testing.T, commit bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(t.Context(), t, blocker, "replay epoch activation blocker")

	first, err := activateReplayEpochTx(ctx, blocker, ReplayEpochInput{
		ActivatedBy: "first-operator",
		Reason:      "first activation awaiting transaction outcome",
	})
	require.NoError(t, err)
	require.True(t, first.Activated)

	input := ReplayEpochInput{
		ActivatedBy: "waiting-operator",
		Reason:      "activation competing with an uncommitted epoch",
	}
	done := make(chan error, 1)

	var second ReplayEpochActivation

	var workers sync.WaitGroup

	workers.Go(func() {
		var activationErr error

		second, activationErr = repo.ActivateReplayEpoch(ctx, input)
		done <- activationErr
	})

	defer func() {
		cancel()
		workers.Wait()
	}()

	waitObservationSettlementLock(ctx, t, pool, blocker.Conn().PgConn().PID(), done)

	if commit {
		require.NoError(t, blocker.Commit(ctx))
	} else {
		require.NoError(t, pgxdb.Rollback(ctx, blocker))
	}

	require.NoError(t, <-done)

	want := first.Epoch

	if !commit {
		want = ReplayEpoch{CutoffReceivedAt: second.Epoch.CutoffReceivedAt, ActivatedBy: input.ActivatedBy, Reason: input.Reason}
		require.False(t, want.CutoffReceivedAt.IsZero())
	}

	requireReplayEpochSettled(ctx, t, repo, input, ReplayEpochActivation{Epoch: want, Activated: !commit}, second)
	assertTableCount(t, pool, "source_observation_replay_epoch", 1)
}

func requireReplayEpochSettled(ctx context.Context, t *testing.T, repo *Repository, input ReplayEpochInput, want, got ReplayEpochActivation) {
	t.Helper()

	require.Equal(t, want, got)

	repeated, err := repo.ActivateReplayEpoch(ctx, input)
	require.NoError(t, err)
	require.Equal(t, ReplayEpochActivation{Epoch: want.Epoch}, repeated)
}

func TestReplayEpochDeadLettersPreEpochAutomaticClaim(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	firstObservationID := publishOne(ctx, t, repo, &proof, "pre-epoch-1")
	secondProof := advanceLease(ctx, t, pool, &proof, time.Minute)
	secondObservationID := publishOne(ctx, t, repo, &secondProof, "pre-epoch-2")

	activateReplayEpoch(t, repo)
	assertReadyDepth(t, repo, claimOptions().Kinds, 0)

	options := claimOptions()

	options.Limit = 1

	batch, err := repo.ClaimBatch(ctx, options)
	if err != nil {
		t.Fatalf("claim after replay epoch activation: %v", err)
	}

	if len(batch.Claims) != 0 {
		t.Fatalf("claims = %#v, want no pre-epoch work", batch.Claims)
	}

	assertReplayEpochDeadLetter(t, pool, firstObservationID)
	assertQueueStatus(t, pool, secondObservationID, string(contract.StatusPending))

	batch, err = repo.ClaimBatch(ctx, options)
	if err != nil {
		t.Fatalf("second claim after replay epoch activation: %v", err)
	}

	if len(batch.Claims) != 0 {
		t.Fatalf("second claims = %#v, want no pre-epoch work", batch.Claims)
	}

	assertReplayEpochDeadLetter(t, pool, secondObservationID)
}

func TestReplayEpochDeadLetterTakesPrecedenceOverExhaustedAttempts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	observationID := publishOne(ctx, t, repo, &proof, "pre-epoch-exhausted")

	if _, err := pool.Exec(ctx, "UPDATE source_observation_queue SET attempt_count = $2 WHERE observation_id = $1",
		observationID, MaxAttempts); err != nil {
		t.Fatalf("exhaust attempts: %v", err)
	}

	activateReplayEpoch(t, repo)

	var before time.Time

	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatalf("load clock: %v", err)
	}

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil {
		t.Fatalf("claim exhausted pre-epoch work: %v", err)
	}

	if len(batch.Claims) != 0 {
		t.Fatalf("claims = %#v, want none", batch.Claims)
	}

	assertReplayEpochDeadLetter(t, pool, observationID)

	var (
		attempts          int
		leased, detailSet bool
		deadLetteredAt    time.Time
	)

	err = pool.QueryRow(ctx, `
		SELECT attempt_count, lease_token IS NOT NULL, last_error_detail IS NOT NULL, dead_lettered_at
		FROM source_observation_queue WHERE observation_id = $1
	`, observationID).Scan(&attempts, &leased, &detailSet, &deadLetteredAt)
	if err != nil {
		t.Fatalf("load dead letter: %v", err)
	}

	if attempts != MaxAttempts || leased || detailSet || deadLetteredAt.Before(before) {
		t.Fatalf("attempts=%d leased=%t detail=%t dead_lettered_at=%s before=%s", attempts, leased, detailSet, deadLetteredAt, before)
	}
}

func TestReplayEpochFencesPreEpochObservationAlreadyClaimed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())
	claim := batch.Claims[0].Claim(batch.ConsumerName)

	activateReplayEpoch(t, repo)

	reconcileCalled := false

	_, err := repo.Finalize(ctx, claim, func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error) {
		reconcileCalled = true

		return ReconcileResult{}, nil
	})
	if err != nil {
		t.Fatalf("finalize pre-epoch claim: %v", err)
	}

	if reconcileCalled {
		t.Fatal("pre-epoch finalize called canonical writer")
	}

	assertReplayEpochDeadLetter(t, pool, claim.ObservationID)
	requireNoReconcileSideEffects(t, pool)
}

func TestReplayEpochAllowsPostEpochObservation(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	activateReplayEpoch(t, repo)

	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	observationID := publishOne(ctx, t, repo, &proof, "post-epoch")

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim post-epoch observation: batch=%#v err=%v", batch, err)
	}

	reconcileCalled := false

	_, err = repo.Finalize(ctx, batch.Claims[0].Claim(batch.ConsumerName), func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error) {
		reconcileCalled = true

		return ReconcileResult{}, nil
	})
	if err != nil {
		t.Fatalf("finalize post-epoch observation: %v", err)
	}

	if !reconcileCalled {
		t.Fatal("post-epoch finalize did not call canonical writer")
	}

	assertQueueStatus(t, pool, observationID, string(contract.StatusProcessed))
}

func activateReplayEpoch(t *testing.T, repo *Repository) ReplayEpochActivation {
	t.Helper()

	activation, err := repo.ActivateReplayEpoch(t.Context(), ReplayEpochInput{
		ActivatedBy: "replay-epoch-test",
		Reason:      "bound historical source observation replay",
	})
	if err != nil {
		t.Fatalf("activate source observation replay epoch: %v", err)
	}

	return activation
}

func assertReplayEpochDeadLetter(t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()

	var status, errorCode string

	if err := pool.QueryRow(t.Context(), `
		SELECT status, COALESCE(last_error_code, '')
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observationID).Scan(&status, &errorCode); err != nil {
		t.Fatalf("load replay epoch dead letter: %v", err)
	}

	if status != string(contract.StatusDeadLetter) || errorCode != replayEpochExpiredCode {
		t.Fatalf("queue status=%s error=%s", status, errorCode)
	}
}
