package sourceobservation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// projectionWriter는 pool과 트랜잭션 모두에서 projection 전환 fixture를 실행하기 위한 최소 능력이다.
type projectionWriter interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// transitionPublishProjection은 API refresh처럼 기존 CURRENT를 RETIRED로 바꾸고(유효 시각은 남긴다) 새 CURRENT에
// target을 옮긴다. 인자 readd가 참이면 같은 target을 새 generation부터 다시 추가된 것으로(ABA) 기록한다.
func transitionPublishProjection(ctx context.Context, t *testing.T, q projectionWriter, previous int64, readd bool) int64 {
	t.Helper()

	if _, err := q.Exec(ctx, `
		UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE generation = $1
	`, previous); err != nil {
		t.Fatalf("retire projection: %v", err)
	}

	var current int64

	if err := q.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		)
		SELECT 'CURRENT', count(*), repeat('b', 64), NOW() + INTERVAL '1 day', NOW()
		FROM youtube_collection_targets WHERE projection_generation = $1
		RETURNING generation
	`, previous).Scan(&current); err != nil {
		t.Fatalf("insert current projection: %v", err)
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until, member_since_generation
		)
		SELECT $1, subject_key, observation_kind, priority, poll_interval_ms, enabled, valid_until,
		       CASE WHEN $3 THEN $1 ELSE member_since_generation END
		FROM youtube_collection_targets
		WHERE projection_generation = $2
	`, current, previous, readd); err != nil {
		t.Fatalf("carry projection targets: %v", err)
	}

	return current
}

// 다른 subject만 바뀐 projection 전환은 이 job의 발행을 막지 않는다(handoff 불변식 1).
func TestPublishAcceptsUnrelatedProjectionChange(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	current := transitionPublishProjection(ctx, t, pool, proof.ProjectionGeneration, false)

	insertPublishTarget(ctx, t, pool, current, "UC_OTHER", contract.KindCommunityPage, true)

	result, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatalf("publish after unrelated churn: %v", err)
	}

	if result.Results[0].Outcome != PublishInserted {
		t.Fatalf("unrelated churn outcome = %s", result.Results[0].Outcome)
	}

	assertPublishSideEffects(t, pool, 1, 1, 1)
}

// 자기 target이 빠졌다가 새 generation에서 다시 추가되면(ABA) member_since가 증명 generation보다 커서 거절된다.
func TestPublishRejectsReaddedOwnTarget(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	transitionPublishProjection(ctx, t, pool, proof.ProjectionGeneration, true)

	_, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("readded target publish error = %v, want ErrTargetDisabled", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)
}

// RETIRED generation의 남은 valid_until은 발행 근거가 아니다. 유효 CURRENT가 없으면 projection stale이다.
func TestPublishRejectsRetiredGenerationWithoutCurrent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE generation = $1
	`, proof.ProjectionGeneration); err != nil {
		t.Fatal(err)
	}

	_, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("retired projection publish error = %v, want ErrProjectionStale", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)
}

// API 전환이 guard를 잡은 동안 대기한 발행은 commit 뒤 새 CURRENT snapshot으로 판정되어, 이어받은 target이면 수락된다.
func TestPublishWaitsForProjectionGuardAndSeesCarriedMembership(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback projection guard transaction: %v", rollbackErr)
		}
	}()

	var guard bool

	if err := tx.QueryRow(ctx, `SELECT guard_key FROM youtube_collection_projection_guard FOR UPDATE`).Scan(&guard); err != nil {
		t.Fatalf("lock projection guard: %v", err)
	}

	transitionPublishProjection(ctx, t, tx, proof.ProjectionGeneration, false)

	input := publishInput(communityEnvelope(t, &proof, "post-1"))
	done := make(chan error, 1)

	go func() {
		_, publishErr := NewRepository(pool).PublishBatch(ctx, input)
		done <- publishErr
	}()

	require.Eventually(t, func() bool {
		var waiting bool

		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database() AND wait_event_type = 'Lock'
			)
		`).Scan(&waiting)

		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatalf("publish waiting on projection guard: %v", err)
	}

	assertPublishSideEffects(t, pool, 1, 1, 1)
}

// 수락 간격은 checkpoint가 실제로 전진한 발행에서만 나온다. 첫 수락·같은 slot 재생·충돌은 간격이 없다.
func TestPublishAcceptedIntervalOnlyForAdvancingCheckpoint(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := NewRepository(pool)
	first := communityEnvelope(t, &proof, "post-1")

	assertAcceptedInterval(t, mustPublishOne(t, repo, first), PublishInserted, false)

	reactivateLease(t, pool, &proof)
	assertAcceptedInterval(t, mustPublishOne(t, repo, first), PublishDuplicate, false)

	next := advanceLease(ctx, t, pool, &proof)
	assertAcceptedInterval(t, mustPublishOne(t, repo, communityEnvelope(t, &next, "post-1")), PublishInserted, true)

	reactivateLease(t, pool, &next)
	assertAcceptedInterval(t, mustPublishOne(t, repo, communityEnvelope(t, &next, "post-2")), PublishCollision, false)
}

// 아직 수락이 없는 subject는 cursor가 없고, 수락 뒤에는 마지막 durable cursor를 돌려준다.
func TestLatestCheckpointCursorReturnsDurableCursor(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := NewRepository(pool)

	cursor, err := repo.LatestCheckpointCursor(ctx, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID)
	if err != nil || cursor != nil {
		t.Fatalf("cursor before publish = %s, %v", cursor, err)
	}

	if _, err = repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatal(err)
	}

	cursor, err = repo.LatestCheckpointCursor(ctx, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID)
	if err != nil {
		t.Fatal(err)
	}

	if err := cursor.Compact(); err != nil {
		t.Fatal(err)
	}

	if string(cursor) != `{"page":1}` {
		t.Fatalf("cursor after publish = %s", cursor)
	}
}

func mustPublishOne(t *testing.T, repo *Repository, envelope *contract.Envelope) PublishedObservation {
	t.Helper()

	result, err := repo.PublishBatch(t.Context(), publishInput(envelope))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("publish result count = %d", len(result.Results))
	}

	return result.Results[0]
}

func assertAcceptedInterval(t *testing.T, got PublishedObservation, outcome PublishOutcome, hasInterval bool) {
	t.Helper()

	if got.Outcome != outcome || got.HasAcceptedInterval != hasInterval || got.AcceptedInterval < 0 {
		t.Fatalf("published = %+v, want outcome=%s interval=%t", got, outcome, hasInterval)
	}

	if !hasInterval && got.AcceptedInterval != 0 {
		t.Fatalf("interval without durable advance: %+v", got)
	}
}
