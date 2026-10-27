package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
)

// 통계 쿼리가 DB에서 기다려도 이미 선점한 작업은 전달되고 관측 작업은 자신의 예산으로 종료한다.
func TestClaimDeliveryDoesNotWaitForQueueMetrics(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()

		if err := pgxdb.Rollback(cleanupCtx, tx); err != nil {
			t.Error(err)
		}
	}()

	if _, err := tx.Exec(ctx, "LOCK TABLE source_observation_queue IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	r := newTestRuntime(t, fakeClaimer{claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
		return sourceobservation.ClaimedBatch{Claims: []sourceobservation.ClaimWork{{ObservationID: 1, LeaseToken: "test"}}}, nil
	}}, fakeConsumer{})

	now := time.Now().Add(24 * time.Hour)

	r.pool, r.now = pool, func() time.Time { return now }
	r.claiming.Store(true)
	r.collectionObservation.last.Store(now.UnixNano())

	claimCtx, claimCancel := context.WithTimeout(ctx, 250*time.Millisecond)

	defer claimCancel()

	if _, err := r.claimTick(claimCtx); err != nil {
		t.Fatal(err)
	}

	if len(r.workCh) != 1 {
		t.Fatal("metrics blocked delivery")
	}

	r.observePendingQueue(ctx)

	if ctx.Err() != nil {
		t.Fatal("metric query exceeded its own budget")
	}
}

// DEAD_LETTER 행이 없는 queue를 관측하면 dead letter 원인의 degraded가 풀린다.
func TestQueueObservationClearsResolvedDeadLetterBacklog(t *testing.T) {
	pool := dbtest.NewPool(t)
	r := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

	r.pool = pool
	r.markDeadLettered()

	r.observePendingQueue(t.Context())

	if r.Degraded() {
		t.Fatal("queue observation without DEAD_LETTER rows left the plane degraded")
	}
}

// 재시작 뒤처럼 이 process가 dead letter를 기록하지 않았어도, queue 관측이 남은 DEAD_LETTER 행을 보면 degraded가 켜진다.
func TestQueueObservationRaisesDegradedForStoredDeadLetter(t *testing.T) {
	pool := dbtest.NewPool(t)
	r := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

	r.pool = pool

	if _, err := pool.Exec(t.Context(), `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	), observation AS (
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
			scheduled_for,observed_at,received_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
			provenance_id,fence_epoch,projection_generation)
		SELECT 'youtubejs','community_page','dead-letter',sha256('dead-letter'::bytea),1,1,now(),now(),now(),
			decode(repeat('a',64),'hex'),'COMPLETE','CONTIGUOUS',payload.id,decode(repeat('c',64),'hex'),
			source_observation_provenance_id('dead-letter','dbtest','community_page'),1,1 FROM payload RETURNING id
	) INSERT INTO source_observation_queue(observation_id,status,dead_lettered_at,last_error_code)
	SELECT id,'DEAD_LETTER',now(),'test_dead_letter' FROM observation`); err != nil {
		t.Fatal(err)
	}

	if r.Degraded() {
		t.Fatal("plane started degraded before observing the queue")
	}

	r.observePendingQueue(t.Context())

	if !r.Degraded() {
		t.Fatal("queue observation with a stored DEAD_LETTER row did not degrade the plane")
	}
}
