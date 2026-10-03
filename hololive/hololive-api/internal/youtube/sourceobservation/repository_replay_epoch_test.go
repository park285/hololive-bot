package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestReplayEpochActivationIsImmutableAndIdempotent(t *testing.T) {
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

func TestReplayEpochDeadLettersPreEpochAutomaticClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	firstObservationID := publishOne(ctx, t, repo, &proof, "pre-epoch-1")
	secondProof := advanceLease(ctx, t, pool, &proof, time.Minute)
	secondObservationID := publishOne(ctx, t, repo, &secondProof, "pre-epoch-2")

	activateReplayEpoch(t, repo)

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

func TestReplayEpochFencesPreEpochObservationAlreadyClaimed(t *testing.T) {
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

func TestReplayEpochRejectsManualAndPendingReplay(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	observationID := publishOne(ctx, t, repo, &proof, "pre-epoch-replay")
	finalizeObservation(ctx, t, repo, observationID)

	pendingRequestID := insertPendingReplayRequest(t, pool, observationID)
	activateReplayEpoch(t, repo)

	manual, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: observationID,
		RequestedBy:   testReplayOperator,
		Reason:        "manual pre-epoch replay must be rejected",
	})
	if err != nil {
		t.Fatalf("request pre-epoch replay: %v", err)
	}

	if manual.Applied || manual.RejectionCode != replayEpochExpiredCode {
		t.Fatalf("manual replay = %#v", manual)
	}

	processed, err := repo.ProcessNextReplay(ctx)
	if err != nil {
		t.Fatalf("process pending pre-epoch replay: %v", err)
	}

	if !processed {
		t.Fatal("pending pre-epoch replay was not processed")
	}

	assertReplayRequestRejected(t, pool, pendingRequestID)
	assertReplayRequestRejected(t, pool, manual.RequestID)
	assertQueueStatus(t, pool, observationID, string(contract.StatusProcessed))
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

func insertPendingReplayRequest(t *testing.T, pool *pgxpool.Pool, observationID int64) int64 {
	t.Helper()

	var requestID int64

	if err := pool.QueryRow(t.Context(), `
		INSERT INTO source_observation_replay_requests (
			observation_id,
			provider,
			observation_kind,
			subject_key,
			observation_key,
			evidence_sha256,
			requested_by,
			reason,
			previous_attempt_count
		)
		SELECT observation.id,
		       observation.provider,
		       observation.observation_kind,
		       observation.subject_key,
		       observation.observation_key,
		       observation.evidence_sha256,
		       $2,
		       $3,
		       queue.attempt_count
		FROM source_observations AS observation
		JOIN source_observation_queue AS queue
		  ON queue.observation_id = observation.id
		WHERE observation.id = $1
		RETURNING id
	`, observationID, testReplayOperator, "pending pre-epoch replay").Scan(&requestID); err != nil {
		t.Fatalf("insert pending replay request: %v", err)
	}

	return requestID
}

func assertReplayRequestRejected(t *testing.T, pool *pgxpool.Pool, requestID int64) {
	t.Helper()

	var status, rejectionCode string

	if err := pool.QueryRow(t.Context(), `
		SELECT status, COALESCE(rejection_code, '')
		FROM source_observation_replay_requests
		WHERE id = $1
	`, requestID).Scan(&status, &rejectionCode); err != nil {
		t.Fatalf("load replay request: %v", err)
	}

	if status != "REJECTED" || rejectionCode != replayEpochExpiredCode {
		t.Fatalf("replay request status=%s rejection=%s", status, rejectionCode)
	}
}
