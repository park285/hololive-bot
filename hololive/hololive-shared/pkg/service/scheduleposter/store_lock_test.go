package scheduleposter

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const lockTestChannel = "UCROQtXcp2loQEmvpe5rhJzQ"

func TestAutoConfirmWaitsForAConcurrentConfirm(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)

	store, err := NewStore(pool)
	require.NoError(t, err)

	postedAt := time.Date(2026, time.November, 7, 3, 0, 0, 0, time.UTC)
	start, end := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC)

	_, err = store.InsertCandidates(ctx, []Candidate{{
		ID: 101, ChannelID: lockTestChannel, XUserID: "2054825907916054528", PostURL: "https://x.com/i/status/101",
		ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig", TextExcerpt: "週間スケジュール", PostedAt: postedAt,
	}}, false)
	require.NoError(t, err)

	_, err = store.InsertThumbnailCandidates(ctx, []ThumbnailCandidate{{
		ChannelID: lockTestChannel, VideoID: "4uuYjSf0rpU", ImageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PostedAt: postedAt,
	}}, true)
	require.NoError(t, err)

	unreviewed, err := store.UnreviewedCandidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, unreviewed, 1)

	admin, err := pool.Begin(ctx)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, pgxdb.Rollback(context.WithoutCancel(t.Context()), admin)) })

	require.NoError(t, lockChannelConfirm(ctx, admin, 101))

	type outcome struct {
		coverage WeekCoverage
		err      error
	}

	done := make(chan outcome, 1)

	go func() {
		coverage, _, autoErr := store.AutoConfirm(ctx, unreviewed[0].ID, unreviewed[0].Revision, start, end)
		done <- outcome{coverage, autoErr}
	}()

	require.Eventually(t, func() bool { return lockWaiting(t, pool) }, 10*time.Second, 20*time.Millisecond,
		"관리자 확정이 잠금을 쥔 동안 자동 확정은 기다린다")

	_, err = changeWeek(ctx, admin, confirmSQL, ReviewPending, 101, 1, start, end, ReviewerAdmin)
	require.NoError(t, err)
	require.NoError(t, admin.Commit(ctx))

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, WeekSame, result.coverage, "잠금을 얻은 뒤 관리자가 확정한 주를 보고 같은 일정을 닫는다")
	case <-time.After(10 * time.Second):
		t.Fatal("auto confirm did not finish after the concurrent confirm committed")
	}
}

func TestAdminChangesWaitForTheChannelLock(t *testing.T) {
	t.Parallel()

	start, end := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC)

	for name, change := range map[string]func(ctx context.Context, store *Store) error{
		"confirm": func(ctx context.Context, store *Store) error {
			_, err := store.Confirm(ctx, 102, 1, start, end)

			return err
		},
		"reject": func(ctx context.Context, store *Store) error {
			_, err := store.Reject(ctx, 102, 1)

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			pool := dbtest.NewPool(t)

			store, err := NewStore(pool)
			require.NoError(t, err)

			postedAt := time.Date(2026, time.November, 7, 3, 0, 0, 0, time.UTC)

			_, err = store.InsertCandidates(ctx, []Candidate{
				{
					ID: 101, ChannelID: lockTestChannel, XUserID: "1", PostURL: "https://x.com/i/status/101",
					ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig", PostedAt: postedAt,
				},
				{
					ID: 102, ChannelID: lockTestChannel, XUserID: "1", PostURL: "https://x.com/i/status/102",
					ImageURL: "https://pbs.twimg.com/media/b.jpg?name=orig", PostedAt: postedAt,
				},
			}, false)
			require.NoError(t, err)

			holder, err := pool.Begin(ctx)
			require.NoError(t, err)

			t.Cleanup(func() { require.NoError(t, pgxdb.Rollback(context.WithoutCancel(t.Context()), holder)) })
			require.NoError(t, lockChannelConfirm(ctx, holder, 101))

			done := make(chan error, 1)

			go func() { done <- change(ctx, store) }()

			require.Eventually(t, func() bool { return lockWaiting(t, pool) }, 10*time.Second, 20*time.Millisecond,
				"같은 멤버의 잠금을 쥔 트랜잭션이 끝날 때까지 기다린다")
			require.NoError(t, holder.Rollback(ctx))

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("change did not finish after the lock was released")
			}
		})
	}
}

func lockWaiting(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()

	var waiting bool

	err := pool.QueryRow(t.Context(), `SELECT count(*) > 0 FROM pg_locks AS waiting
		JOIN pg_stat_activity AS activity ON activity.pid = waiting.pid
		WHERE NOT waiting.granted AND activity.datname = current_database()`).Scan(&waiting)

	return err == nil && waiting
}

func TestHandOverWaitsForAnAutoJudgmentInProgress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)

	store, err := NewStore(pool)
	require.NoError(t, err)

	_, err = store.InsertCandidates(ctx, []Candidate{{
		ID: 101, ChannelID: lockTestChannel, XUserID: "1", PostURL: "https://x.com/i/status/101",
		ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig", PostedAt: time.Date(2026, time.November, 7, 3, 0, 0, 0, time.UTC),
	}}, true)
	require.NoError(t, err)

	judging, err := pool.Begin(ctx)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, pgxdb.Rollback(context.WithoutCancel(t.Context()), judging)) })
	require.NoError(t, lockUnreviewed(ctx, judging, 101))

	handed := make(chan int64, 1)

	go func() {
		count, handErr := store.HandOverUnreviewed(ctx, time.Now().Add(time.Hour), ReviewReasonStale)
		if handErr != nil {
			count = -1
		}

		handed <- count
	}()

	require.Eventually(t, func() bool { return lockWaiting(t, pool) }, 10*time.Second, 20*time.Millisecond,
		"자동 판정이 후보 행을 잠근 동안 일괄 인계는 기다린다")

	_, err = mutate(ctx, judging, "update schedule poster", rejectSQL, 101, 1, ReviewerAuto)
	require.NoError(t, err)
	require.NoError(t, judging.Commit(ctx))

	select {
	case count := <-handed:
		require.Zero(t, count, "그 사이 자동 거절된 후보는 넘기지 않는다")
	case <-time.After(10 * time.Second):
		t.Fatal("hand over did not finish after the judgment committed")
	}
}
