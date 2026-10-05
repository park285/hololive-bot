package sourceobservation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestScheduleBatchPreservesConcurrentLiveInsertion(t *testing.T) {
	for _, status := range []domain.LiveStatus{domain.LiveStatusLive, domain.LiveStatusEnded} {
		t.Run(string(status), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

			defer cancel()

			observation, fixture := scheduleBatchFixture(1)
			scheduleTx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackPublishTestTx(ctx, t, scheduleTx, "concurrent schedule")

			state, err := loadScheduleState(ctx, scheduleTx, fixture.Items)
			require.NoError(t, err)
			require.Empty(t, state.Sessions)

			decision, err := schedule.Reduce(state, schedule.Evidence{
				GroupKey: fixture.Items[0].GroupKey, Provider: observation.Provider,
				Items: fixture.Items, EffectiveAt: observation.EffectiveAt, ReceivedAt: observation.EffectiveAt,
			})
			require.NoError(t, err)

			// Schedule 조회 때 없던 영상을 다른 writer가 먼저 생성하고 아직 커밋하지 않았다.
			liveTx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackPublishTestTx(ctx, t, liveTx, "concurrent live")

			newer := observation.EffectiveAt.Add(time.Minute)
			session := live.SessionState{
				VideoID: fixture.Items[0].VideoID, ChannelID: testChannelID, Status: status,
				Title: "new live title", TitleObservedAt: new(newer), ScheduleObservedAt: new(newer),
				ScheduledStartTime: new(newer), LastSeenAt: newer, IsPremiere: new(true),
				LifecycleOrigin: live.OriginObserved, StatusObservedAt: new(newer),
			}
			statement := liveSessionStatement(&session, false)

			_, err = liveTx.Exec(ctx, statement.SQL, statement.Args...)
			require.NoError(t, err)

			pid := scheduleTx.Conn().PgConn().PID()
			done := make(chan error, 1)

			var writers sync.WaitGroup

			writers.Go(func() { done <- persistScheduleDecision(ctx, scheduleTx, &observation, &decision) })

			defer func() {
				cancel()
				writers.Wait()
			}()

			waitScheduleInsertionLock(ctx, t, pool, pid, done)
			require.NoError(t, liveTx.Commit(ctx))
			require.NoError(t, <-done)
			require.NoError(t, scheduleTx.Commit(ctx))

			assertScheduleConcurrentSession(t, pool, &session)
			assertTableCount(t, pool, "youtube_schedule_items", 1)
		})
	}
}

func waitScheduleInsertionLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pid uint32, done <-chan error) {
	t.Helper()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		var waiting bool

		require.NoError(t, pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&waiting))

		if waiting {
			return
		}

		select {
		case err := <-done:
			t.Fatalf("schedule did not wait for concurrent insertion: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertScheduleConcurrentSession(t *testing.T, pool *pgxpool.Pool, session *live.SessionState) {
	t.Helper()

	var gotStatus, title, origin string

	var isPremiere bool

	var gotSeen, gotTitleClock, gotSchedule time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, title, lifecycle_origin, is_premiere, last_seen_at, title_observed_at, scheduled_start_time
		FROM youtube_live_sessions WHERE video_id=$1
	`, session.VideoID).Scan(&gotStatus, &title, &origin, &isPremiere, &gotSeen, &gotTitleClock, &gotSchedule))
	require.Equal(t, string(session.Status), gotStatus)
	require.Equal(t, session.Title, title)
	require.Equal(t, string(session.LifecycleOrigin), origin)
	require.True(t, isPremiere)
	require.True(t, gotSeen.Equal(session.LastSeenAt))
	require.True(t, gotTitleClock.Equal(*session.TitleObservedAt))
	require.True(t, gotSchedule.Equal(*session.ScheduledStartTime))
}

func TestScheduleStateKeepsSessionLockedUntilPersistence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	observation, decision := scheduleBatchFixture(1)

	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return persistScheduleDecision(ctx, tx, &observation, &decision)
	}))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(ctx, t, tx, "schedule snapshot")

	state, err := loadScheduleState(ctx, tx, decision.Items)
	require.NoError(t, err)
	require.Len(t, state.Sessions, 1)

	// 다른 writer가 잠금을 가져갈 수 없어야 조회부터 저장까지 같은 상태를 보호한다.
	var unlocked int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM (SELECT video_id FROM youtube_live_sessions FOR UPDATE SKIP LOCKED) available
	`).Scan(&unlocked))
	require.Zero(t, unlocked)
	require.NoError(t, persistScheduleDecision(ctx, tx, &observation, &decision))
	require.NoError(t, tx.Commit(ctx))
}
