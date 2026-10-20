package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// 배제 경로가 exclusive 전역 잠금을 쥔 동안 관측 writer는 claim보다 먼저 shared 잠금에서 기다리고,
// 원본과 수명 모델 갱신은 함께 커밋되거나 함께 되돌려진다.
func TestFinalizeRuntimeGuardPrecedesClaimAndRollsBackProjection(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())
			work := batch.Claims[0]
			holder, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackPublishTestTx(t.Context(), t, holder, "runtime guard holder")

			_, err = holder.Exec(ctx, `SELECT lock_youtube_live_runtime_state()`)
			require.NoError(t, err)

			rejected := errors.New("reject canonical transaction after projection refresh")
			done := make(chan error, 1)

			var workers sync.WaitGroup

			workers.Go(func() {
				_, finalizeErr := NewRepository(pool).Finalize(ctx, work.Claim(batch.ConsumerName), runtimeProjectionWrite(rollback, rejected))
				done <- finalizeErr
			})

			defer func() {
				cancel()
				workers.Wait()
			}()

			waitObservationSettlementLock(ctx, t, pool, holder.Conn().PgConn().PID(), done)

			// 전역 잠금 대기자가 claim을 먼저 잡았다면 NOWAIT가 실패한다.
			_, err = holder.Exec(ctx, `SELECT observation_id FROM source_observation_queue
				WHERE observation_id=$1 FOR UPDATE NOWAIT`, work.ObservationID)
			require.NoError(t, err)
			require.NoError(t, holder.Commit(ctx))

			err = <-done

			if rollback {
				require.ErrorIs(t, err, rejected)
				assertTableCount(t, pool, "youtube_live_sessions", 0)
				assertTableCount(t, pool, "youtube_live_runtime_state", 0)
				requireQueueClaimIntact(ctx, t, pool, work, "runtime projection")
			} else {
				require.NoError(t, err)
				assertTableCount(t, pool, "youtube_live_sessions", 1)
				assertTableCount(t, pool, "youtube_live_runtime_state", 1)
				assertQueueStatus(t, pool, work.ObservationID, string(contract.StatusProcessed))
			}

			require.Equal(t, [3]int64{}, dbtest.LiveRuntimeStateDrift(t, pool))

			// 성공과 롤백 모두 다음 배제 경로가 같은 전역 잠금을 다시 획득할 수 있어야 한다.
			_, err = pool.Exec(ctx, `SELECT lock_youtube_live_runtime_state()`)
			require.NoError(t, err)
		})
	}
}

// 관측 writer가 shared 전역 잠금을 쥔 동안 retention은 exclusive 잠금에서 후보를 잠그기 전에 기다리고,
// 증거 FK 해제는 수명 모델 입력이 아니므로 갱신 없이도 모델이 오라클과 일치한다.
func TestEvidenceRetentionGuardsBeforeCandidateAndPreservesAvailability(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			repo := NewRepository(pool)
			id := seedRuntimeGuardRetention(ctx, t, pool, repo)

			var before string

			require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(s)::text FROM youtube_live_runtime_state s WHERE video_id='vid-a'`).Scan(&before))

			if rollback {
				_, err := pool.Exec(ctx, `CREATE FUNCTION reject_retention_commit() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN RAISE EXCEPTION 'retention rollback probe'; END $$;
					CREATE TRIGGER reject_retention_commit AFTER DELETE ON source_observations
					FOR EACH STATEMENT EXECUTE FUNCTION reject_retention_commit()`)
				require.NoError(t, err)
			}

			holder, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackPublishTestTx(t.Context(), t, holder, "retention runtime guard holder")

			require.NoError(t, lockLiveRuntimeState(ctx, holder))

			done := make(chan error, 1)

			var (
				deleted int64
				workers sync.WaitGroup
			)

			workers.Go(func() {
				var deleteErr error

				deleted, deleteErr = repo.deleteEvidenceBatch(ctx, RetentionConfig{
					EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{contract.KindCommunityPage: time.Hour},
					BatchSize:         10,
				}, time.Now().Add(48*time.Hour))

				done <- deleteErr
			})

			defer func() {
				cancel()
				workers.Wait()
			}()

			waitObservationSettlementLock(ctx, t, pool, holder.Conn().PgConn().PID(), done)

			// retention 후보를 선잠그면 FK 갱신 writer와 반대 순서가 되므로 NOWAIT로 검증한다.
			_, err = holder.Exec(ctx, `SELECT id FROM source_observations WHERE id=$1 FOR UPDATE NOWAIT`, id)
			require.NoError(t, err)

			_, err = holder.Exec(ctx, `SELECT video_id FROM youtube_video_availability WHERE video_id='vid-a' FOR UPDATE NOWAIT`)
			require.NoError(t, err)
			require.NoError(t, holder.Commit(ctx))

			err = <-done
			assertRuntimeRetentionOutcome(ctx, t, pool, id, deleted, rollback, err)

			var after string

			require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(s)::text FROM youtube_live_runtime_state s WHERE video_id='vid-a'`).Scan(&after))
			require.Equal(t, before, after, "retention must not erase retained availability clocks")
			require.Equal(t, [3]int64{}, dbtest.LiveRuntimeStateDrift(t, pool))
		})
	}
}

func runtimeProjectionWrite(rollback bool, rejected error) ReconcileWrite {
	return func(ctx context.Context, tx dbx.Tx, _ *Observation) (ReconcileResult, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO youtube_live_sessions
			(video_id, channel_id, status, title) VALUES ($1, $2, 'LIVE', 'guarded live')`, testVideoID, testChannelID); err != nil {
			return ReconcileResult{}, fmt.Errorf("insert live session: %w", err)
		}

		if rollback {
			return ReconcileResult{}, rejected
		}

		return ReconcileResult{LiveRuntimeVideoIDs: []string{testVideoID}}, nil
	}
}

func seedRuntimeGuardRetention(ctx context.Context, t *testing.T, pool *pgxpool.Pool, repo *Repository) int64 {
	t.Helper()

	ids := publishProcessedObservations(ctx, t, pool, repo, 1)
	_, err := pool.Exec(ctx, `
		DELETE FROM source_observation_queue;
		UPDATE youtube_collection_projection_generations SET status='RETIRED';
		UPDATE youtube_collection_job_leases SET slot_state='IDLE', owner_instance=NULL,
			lease_expires_at=NULL, retry_not_before=NULL;
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', 'retained availability');
		INSERT INTO youtube_video_availability (
			video_id, channel_id, provider, identity_confirmed, availability, method,
			observation_id, evidence_sha256, scheduled_for, effective_at, observed_at, received_at
		) SELECT 'vid-a', 'UC_TEST', 'youtubejs', true, 'PUBLIC', 'player_public',
			id, encode(evidence_sha256, 'hex'), scheduled_for, scheduled_for, observed_at, received_at
		FROM source_observations;
	`)
	require.NoError(t, err)
	dbtest.RefreshAllLiveRuntimeState(t, pool)

	return ids[0]
}

func assertRuntimeRetentionOutcome(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id, deleted int64, rollback bool, result error) {
	t.Helper()

	var (
		observationID *int64
		retained      int64
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT observation_id FROM youtube_video_availability WHERE video_id='vid-a'`).Scan(&observationID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observations`).Scan(&retained))

	if rollback {
		require.ErrorContains(t, result, "retention rollback probe")
		require.Zero(t, deleted)
		require.NotNil(t, observationID)
		require.Equal(t, id, *observationID)
		require.EqualValues(t, 1, retained)
	} else {
		require.NoError(t, result)
		require.EqualValues(t, 1, deleted)
		require.Nil(t, observationID)
		require.Zero(t, retained)
	}
}
