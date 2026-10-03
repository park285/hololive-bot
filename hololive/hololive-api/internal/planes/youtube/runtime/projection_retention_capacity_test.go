package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestProjectionRetentionDrainsProgressWithinOneDeadline(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	var (
		calls       int
		deadline    time.Time
		cutoffClock time.Time
	)

	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(ctx context.Context, now time.Time, _ time.Duration, batchSize int) (targetprojection.RetentionResult, error) {
			calls++

			gotDeadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Equal(t, 1000, batchSize)

			if calls == 1 {
				deadline, cutoffClock = gotDeadline, now
			} else {
				require.Equal(t, deadline, gotDeadline, "later batches must not renew the tick budget")
				require.Equal(t, cutoffClock, now, "all batches must use the same retention cutoff")
			}

			switch calls {
			case 1:
				return targetprojection.RetentionResult{ReasonsDeleted: 600, TargetsDeleted: 400}, nil
			case 2:
				return targetprojection.RetentionResult{TargetsDeleted: 200, GenerationsDeleted: 1}, nil
			case 3:
				return targetprojection.RetentionResult{LeasesDeleted: 1}, nil
			default:
				return targetprojection.RetentionResult{}, nil
			}
		},
	}

	require.NoError(t, runtime.retainProjections(t.Context()))
	require.Equal(t, 4, calls, "a partly filled generation batch does not mean the backlog is empty")
}

func TestProjectionRetentionBoundsContinuousProgress(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	calls := 0

	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error) {
			calls++
			if calls > 64 {
				return targetprojection.RetentionResult{}, errors.New("tick exceeded its row budget")
			}

			return targetprojection.RetentionResult{ReasonsDeleted: 1000}, nil
		},
	}

	require.NoError(t, runtime.retainProjections(t.Context()))
	require.Equal(t, 64, calls)
}

func TestProjectionRetentionPreservesCommittedCountsOnFailure(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	wantErr := errors.New("projection batch failed")
	calls := 0
	deletedTargets := youtubeRetentionDeletedTotal.WithLabelValues("youtube_collection_targets")
	deletedLeases := youtubeRetentionDeletedTotal.WithLabelValues("youtube_collection_job_leases")
	failures := youtubeRetentionErrorsTotal.WithLabelValues("youtube_collection_projection_generations")
	beforeTargets, beforeLeases, beforeErrors := testutil.ToFloat64(deletedTargets), testutil.ToFloat64(deletedLeases), testutil.ToFloat64(failures)

	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error) {
			calls++
			if calls == 1 {
				return targetprojection.RetentionResult{TargetsDeleted: 400}, nil
			}

			return targetprojection.RetentionResult{LeasesDeleted: 3}, wantErr
		},
	}

	require.ErrorIs(t, runtime.retainProjections(t.Context()), wantErr)
	require.Equal(t, 2, calls, "a failed batch must not be retried")
	require.InDelta(t, beforeTargets+400, testutil.ToFloat64(deletedTargets), 0)
	require.InDelta(t, beforeLeases+3, testutil.ToFloat64(deletedLeases), 0)
	require.InDelta(t, beforeErrors+1, testutil.ToFloat64(failures), 0)
}

func TestProjectionRetentionStopsBeforeNextBatchOnCancellation(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	calls := 0

	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error) {
			calls++

			cancel()

			return targetprojection.RetentionResult{TargetsDeleted: 10}, nil
		},
	}

	require.ErrorIs(t, runtime.retainProjections(ctx), context.Canceled)
	require.Equal(t, 1, calls)
}

func TestProjectionRetentionDeadlineKeepsSourceCleanupIndependent(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.Config.Retention.Enabled = true
	runtime.Config.TransactionTimeout = 20 * time.Millisecond

	calls := 0
	sourceCalled := false

	runtime.projectionRetainer = fakeProjectionRetainer{
		retain: func(ctx context.Context, _ time.Time, _ time.Duration, _ int) (targetprojection.RetentionResult, error) {
			calls++
			if calls == 1 {
				return targetprojection.RetentionResult{TargetsDeleted: 1}, nil
			}

			<-ctx.Done()

			return targetprojection.RetentionResult{}, ctx.Err()
		},
	}
	runtime.retainer = fakeRetainer{
		tick: func(ctx context.Context, _ sourceobservation.RetentionConfig, _ time.Time) (sourceobservation.RetentionResult, error) {
			sourceCalled = true

			require.NoError(t, ctx.Err())

			return sourceobservation.RetentionResult{}, nil
		},
	}

	require.ErrorIs(t, runtime.retentionTick(t.Context()), context.DeadlineExceeded)
	require.Equal(t, 2, calls)
	require.True(t, sourceCalled)
}

func TestProjectionRetentionCatchesUpWithObservedGenerationVolume(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	// 운영에서 세대마다 약 600개 target과 reason이 생성됐다. 16세대는
	// 120초당 실측 약 12세대보다 크며, CURRENT와 유효 lease는 별도로 보존한다.
	generations := make([]int64, 0, 18)

	for i := range 18 {
		status := "RETIRED"

		if i == 17 {
			status = "CURRENT"
		}

		var generation int64

		err := pool.QueryRow(ctx, `
			INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
			VALUES ($1,600,repeat('a',64),statement_timestamp()-INTERVAL '8 days',statement_timestamp()-INTERVAL '9 days') RETURNING generation
		`, status).Scan(&generation)
		require.NoError(t, err)

		generations = append(generations, generation)
		_, err = pool.Exec(ctx, `
			INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
			SELECT $1,'capacity:'||n,'community_page',50,60000,true,statement_timestamp()-INTERVAL '8 days'
			FROM generate_series(1,600) n;
		`, generation)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO youtube_collection_target_reasons(projection_generation,subject_key,observation_kind,reason_kind,reason_key)
			SELECT $1,'capacity:'||n,'community_page','notification_target','capacity:'||n
			FROM generate_series(1,600) n
		`, generation)
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
			projection_generation,poll_interval_ms,slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at)
		VALUES ('capacity:protected','youtubejs','SUBJECT','youtubejs_community','capacity:1',
			$1,60000,'ACTIVE',statement_timestamp(),statement_timestamp(),'capacity-collector',statement_timestamp()+INTERVAL '1 hour')
	`, generations[16])
	require.NoError(t, err)

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})

	runtime.now = time.Now
	runtime.projectionRetainer = refresher

	started := time.Now()

	require.NoError(t, runtime.retainProjections(ctx))

	elapsed := time.Since(started)

	var remaining int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_projection_generations`).Scan(&remaining))
	require.Equal(t, 2, remaining, "expired generations must drain while CURRENT and leased generations survive")

	for _, generation := range generations[16:] {
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_targets WHERE projection_generation=$1`, generation).Scan(&remaining))
		require.Equal(t, 600, remaining)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_target_reasons WHERE projection_generation=$1`, generation).Scan(&remaining))
		require.Equal(t, 600, remaining)
	}

	t.Logf("retention removed 16 expired generations / 19,200 child rows in %s", elapsed)
}
