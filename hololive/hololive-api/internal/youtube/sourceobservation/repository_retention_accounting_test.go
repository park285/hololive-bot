package sourceobservation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestRetentionKeepsCommittedCountsOnLaterFailure(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := NewRepository(pool)
	ids := publishProcessedObservations(ctx, t, pool, repo, 2)
	ageQueueTerminal(t, pool, ids, 48*time.Hour)

	_, err := pool.Exec(ctx, `ALTER TABLE source_observation_collisions RENAME TO unavailable_collisions`)
	require.NoError(t, err)

	result, err := repo.RunRetentionTick(ctx, RetentionConfig{
		QueueProcessedAge: 24 * time.Hour, CollisionAge: 24 * time.Hour, BatchSize: 1,
	}, time.Now().UTC())
	require.Error(t, err)
	require.Equal(t, "source_observation_collisions", result.FailedTable)
	require.EqualValues(t, 1, result.Deleted)
	require.Len(t, result.ByTable, 1)
	require.Equal(t, "source_observation_queue", result.ByTable[0].Table)
	require.EqualValues(t, 1, result.ByTable[0].Deleted)
	assertTableCount(t, pool, "source_observation_queue", 1)
	assertTableCount(t, pool, "source_observations", 2)
}

func TestRetentionBacklogMeasuresRemainingParentsAndResets(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := NewRepository(pool)
	now := time.Now().UTC()
	// 배치 1건이면 한 tick은 창 상한만큼만 지우므로, 만료 원본을 그보다 두 건 많이 둔다(i번째는 100-i일 전).
	expired := sourceRetentionMaxWindows + 2
	ids := publishProcessedObservations(ctx, t, pool, repo, expired+1)

	for i := range expired {
		ageObservations(t, pool, ids[i:i+1], time.Duration(100-i)*24*time.Hour)
	}

	ageObservations(t, pool, ids[expired:], 89*24*time.Hour)

	_, err := pool.Exec(ctx, `DELETE FROM source_observation_queue;
		UPDATE youtube_collection_projection_generations SET status='RETIRED';
		UPDATE youtube_collection_job_leases SET slot_state='IDLE',owner_instance=NULL,lease_expires_at=NULL,retry_not_before=NULL`)
	require.NoError(t, err)

	cfg := RetentionConfig{
		EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{contract.KindCommunityPage: 90 * 24 * time.Hour},
		BatchSize:         1,
	}
	result, err := repo.RunRetentionTick(ctx, cfg, now)
	require.NoError(t, err)

	// 기대 나이는 남은 가장 오래된 원본의 실제 received_at으로 계산한다. 원본을 하나씩 늙히는 동안 흐른 시간이
	// tick 기준 시각과의 차이에 섞이지 않게 한다.
	var oldestRemaining time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT received_at FROM source_observations WHERE id=$1`, ids[sourceRetentionMaxWindows]).
		Scan(&oldestRemaining))

	part := evidenceRetentionPart(t, result)
	require.True(t, part.BacklogKnown)
	require.InDelta(t, now.Sub(oldestRemaining).Seconds(), part.BacklogAge.Seconds(), 1)
	require.EqualValues(t, sourceRetentionMaxWindows, part.Deleted)

	result, err = repo.RunRetentionTick(ctx, cfg, now)
	require.NoError(t, err)

	part = evidenceRetentionPart(t, result)
	require.True(t, part.BacklogKnown)
	require.Zero(t, part.BacklogAge)
	assertTableCount(t, pool, "source_observations", 1)
}

func TestRetentionBacklogDoesNotClaimEmptyBeyondProtectedScanLimit(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := NewRepository(pool)
	ids := publishProcessedObservations(ctx, t, pool, repo, 3)
	ageObservations(t, pool, ids, 48*time.Hour)

	// 큐가 남은 앞의 두 원본은 보호된다. 한도 뒤의 적격 원본을 보지 못한 경우는 미확정이다.
	ageObservations(t, pool, ids[:2], 72*time.Hour)

	_, err := pool.Exec(ctx, `DELETE FROM source_observation_queue WHERE observation_id=$1`, ids[2])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status='RETIRED';
		UPDATE youtube_collection_job_leases SET slot_state='IDLE',owner_instance=NULL,lease_expires_at=NULL,retry_not_before=NULL`)
	require.NoError(t, err)

	for _, tc := range []struct {
		limit int
		known bool
	}{
		{limit: 2, known: false},
		{limit: 3, known: true},
	} {
		var (
			oldest *time.Time
			known  bool
		)

		require.NoError(t, pool.QueryRow(ctx, mustSQL("repository_retention_backlog_evidence.sql"),
			[]string{string(contract.KindCommunityPage)}, []time.Time{time.Now().Add(-24 * time.Hour)}, tc.limit).Scan(&oldest, &known))
		require.Equal(t, tc.known, known)

		if known {
			require.NotNil(t, oldest)
			require.InDelta(t, (48 * time.Hour).Seconds(), time.Since(*oldest).Seconds(), 1)
		}
	}
}

func evidenceRetentionPart(t *testing.T, result RetentionResult) RetentionResult {
	t.Helper()

	for _, part := range result.ByTable {
		if part.Table == "source_observations" {
			return part
		}
	}

	t.Fatalf("missing source_observations retention table in %+v", result)

	return RetentionResult{}
}
