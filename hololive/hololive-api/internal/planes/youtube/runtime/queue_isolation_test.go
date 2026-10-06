package runtime

import (
	"context"
	"testing"
	"time"

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

		if err := tx.Rollback(cleanupCtx); err != nil {
			t.Error(err)
		}
	}()

	if _, err := tx.Exec(ctx, "LOCK TABLE source_observation_queue IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	r := newTestRuntime(fakeClaimer{claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
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
