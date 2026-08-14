package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestRetentionTickDeletesAtMostBatchSize(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	ids := publishProcessedObservations(t, ctx, pool, repo, 3)
	ageObservations(t, pool, ids, 48*time.Hour)
	ageQueueTerminal(t, pool, ids, 48*time.Hour)
	result, err := repo.RunRetentionTick(ctx, RetentionConfig{
		QueueProcessedAge: 24 * time.Hour,
		BatchSize:         1,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("retention tick: %v", err)
	}
	if result.Table != "source_observation_queue" || result.Deleted != 1 {
		t.Fatalf("result = %#v, want one queue row", result)
	}
	assertTableCount(t, pool, "source_observation_queue", 2)
	assertTableCount(t, pool, "source_observations", 3)
}

func TestRetentionTickDoesNotDeleteActiveOrPendingReplayEvidence(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	pendingID := publishOne(t, ctx, repo, proof, "post-pending")
	finalizeObservation(t, ctx, repo, pendingID)
	insertPendingReplay(t, pool, pendingID)

	proof = advanceLease(t, pool, proof, time.Minute)
	activeID := publishOne(t, ctx, repo, proof, "post-active")
	if _, err := repo.ClaimBatch(ctx, claimOptions()); err != nil {
		t.Fatalf("claim active: %v", err)
	}

	proof = advanceLease(t, pool, proof, time.Minute)
	candidateID := publishOne(t, ctx, repo, proof, "post-candidate")
	finalizeObservation(t, ctx, repo, candidateID)
	insertLiveEndCandidate(t, pool, candidateID)

	ageObservations(t, pool, []int64{pendingID, activeID, candidateID}, 48*time.Hour)
	result, err := repo.RunRetentionTick(ctx, RetentionConfig{
		BatchSize: 10,
		EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{
			contract.KindCommunityPage: 24 * time.Hour,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("retention tick: %v", err)
	}
	if result.Deleted != 0 {
		t.Fatalf("protected evidence deleted: %#v", result)
	}
	assertTableCount(t, pool, "source_observations", 3)
}

func TestRetentionTickDoesNotDeleteEvidenceWhileQueueRemains(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	ids := publishProcessedObservations(t, ctx, pool, repo, 1)
	ageObservations(t, pool, ids, 48*time.Hour)
	result, err := repo.RunRetentionTick(ctx, RetentionConfig{
		BatchSize: 10,
		EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{
			contract.KindCommunityPage: 24 * time.Hour,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("retention tick: %v", err)
	}
	if result.Deleted != 0 {
		t.Fatalf("evidence deleted while queue remained: %#v", result)
	}
	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_queue", 1)
}

func TestRetentionTickProcessedAndDLQDurationsDiffer(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	processedID := publishOne(t, ctx, repo, proof, "post-processed")
	finalizeObservation(t, ctx, repo, processedID)
	proof = advanceLease(t, pool, proof, time.Minute)
	dlqID := publishOne(t, ctx, repo, proof, "post-dlq")
	deadLetterObservation(t, ctx, repo, dlqID)
	ageQueueTerminal(t, pool, []int64{processedID, dlqID}, 36*time.Hour)

	result, err := repo.RunRetentionTick(ctx, RetentionConfig{
		QueueProcessedAge: 24 * time.Hour,
		QueueDLQAge:       48 * time.Hour,
		BatchSize:         10,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("retention tick: %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("result = %#v, want only processed queue row", result)
	}
	assertQueueStatus(t, pool, processedID, "")
	assertQueueStatus(t, pool, dlqID, string(contract.StatusDeadLetter))
}

func publishProcessedObservations(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	count int,
) []int64 {
	t.Helper()
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		if i > 0 {
			proof = advanceLease(t, pool, proof, time.Minute)
		}
		id := publishOne(t, ctx, repo, proof, "post-batch")
		finalizeObservation(t, ctx, repo, id)
		ids = append(ids, id)
	}
	return ids
}

func publishOne(
	t *testing.T,
	ctx context.Context,
	repo *Repository,
	proof contract.LeaseProof,
	postID string,
) int64 {
	t.Helper()
	published, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, postID)))
	if err != nil || len(published.Results) != 1 {
		t.Fatalf("publish %s: %#v err=%v", postID, published, err)
	}
	return published.Results[0].ObservationID
}

func finalizeObservation(t *testing.T, ctx context.Context, repo *Repository, observationID int64) {
	t.Helper()
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 || batch.Observations[0].ID != observationID {
		t.Fatalf("claim %d: %#v err=%v", observationID, batch, err)
	}
	if _, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observationID,
		LeaseToken:    batch.Observations[0].LeaseToken,
	}, func(context.Context, dbx.Tx, Observation) (ReconcileResult, error) {
		return ReconcileResult{}, nil
	}); err != nil {
		t.Fatalf("finalize %d: %v", observationID, err)
	}
}

func deadLetterObservation(t *testing.T, ctx context.Context, repo *Repository, observationID int64) {
	t.Helper()
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 || batch.Observations[0].ID != observationID {
		t.Fatalf("claim dead letter %d: %#v err=%v", observationID, batch, err)
	}
	if err := repo.DeadLetter(ctx, DeadLetterInput{
		ObservationID: observationID,
		LeaseToken:    batch.Observations[0].LeaseToken,
		ErrorCode:     "test_dead_letter",
		ErrorDetail:   "retention duration fixture",
	}); err != nil {
		t.Fatalf("dead letter %d: %v", observationID, err)
	}
}

func ageObservations(t *testing.T, pool *pgxpool.Pool, ids []int64, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE source_observations
		SET received_at = NOW() - ($2 * INTERVAL '1 millisecond')
		WHERE id = ANY($1)
	`, ids, age.Milliseconds()); err != nil {
		t.Fatalf("age observations: %v", err)
	}
}

func ageQueueTerminal(t *testing.T, pool *pgxpool.Pool, ids []int64, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE source_observation_queue
		SET processed_at = CASE WHEN status = 'PROCESSED' THEN NOW() - ($2 * INTERVAL '1 millisecond') ELSE processed_at END,
		    dead_lettered_at = CASE WHEN status = 'DEAD_LETTER' THEN NOW() - ($2 * INTERVAL '1 millisecond') ELSE dead_lettered_at END,
		    updated_at = NOW() - ($2 * INTERVAL '1 millisecond')
		WHERE observation_id = ANY($1)
	`, ids, age.Milliseconds()); err != nil {
		t.Fatalf("age queue: %v", err)
	}
}

func insertPendingReplay(t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO source_observation_replay_requests (
			observation_id, provider, observation_kind, subject_key, observation_key,
			evidence_sha256, requested_by, reason, previous_attempt_count
		)
		SELECT id, provider, observation_kind, subject_key, observation_key,
		       evidence_sha256, 'test-operator', 'hold evidence', 0
		FROM source_observations
		WHERE id = $1
	`, observationID); err != nil {
		t.Fatalf("insert pending replay: %v", err)
	}
}

func insertLiveEndCandidate(t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_live_reconciliation_heads (
			video_id, status, end_candidate_kind, end_candidate_observation_id, next_end_check_at
		) VALUES ('video-end-candidate', 'LIVE', 'SCOPED_ABSENCE', $1, NOW() + INTERVAL '1 minute')
	`, observationID); err != nil {
		t.Fatalf("insert live end candidate: %v", err)
	}
}

func assertQueueStatus(t *testing.T, pool *pgxpool.Pool, observationID int64, want string) {
	t.Helper()
	var status *string
	if err := pool.QueryRow(context.Background(), `
		SELECT status FROM source_observation_queue WHERE observation_id = $1
	`, observationID).Scan(&status); err != nil && want != "" {
		t.Fatalf("load queue %d: %v", observationID, err)
	}
	got := ""
	if status != nil {
		got = *status
	}
	if got != want {
		t.Fatalf("queue %d status = %q, want %q", observationID, got, want)
	}
}
