package sourceobservation

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 같은 상태의 반복 관측은 last_seen_at·status_observed_at만 바꾼다. 이 열이 인덱스 key(식 인덱스가 참조하는 열 포함)에
// 들어 있으면 갱신마다 새 인덱스 항목을 쓰고 HOT이 막힌다. 예정 시각이 있는 세션은 방송 이력 정렬값
// (history_sort_at)도 그대로이므로 모든 갱신이 HOT이어야 한다.
func TestLiveSessionRepeatedObservationIsHeapOnlyUpdate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	observed := time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)
	session := &live.SessionState{
		VideoID:            "hot-update-01",
		ChannelID:          testChannelID,
		Status:             domain.LiveStatusUpcoming,
		LifecycleOrigin:    live.OriginObserved,
		Title:              "예정 방송",
		ScheduledStartTime: new(observed.Add(time.Hour)),
		LastSeenAt:         observed,
		StatusObservedAt:   new(observed),
		ScheduleObservedAt: new(observed),
		TitleObservedAt:    new(observed),
	}

	insert, _ := liveSessionWrite(nil, session)
	_, err := pool.Exec(ctx, insert.SQL, insert.Args...)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, tx)) }()

	const refreshes = 5

	stored := *session

	stored.Present = true

	for i := range refreshes {
		seen := observed.Add(time.Duration(i+1) * time.Minute)
		next := stored

		next.LastSeenAt = seen
		next.StatusObservedAt = new(seen)

		statement, write := liveSessionWrite(map[string]live.SessionState{stored.VideoID: stored}, &next)
		require.True(t, write)

		tag, execErr := tx.Exec(ctx, statement.SQL, statement.Args...)
		require.NoError(t, execErr)
		require.EqualValues(t, 1, tag.RowsAffected(), "refresh %d must update the session", i)

		stored = next
	}

	var updated, hot int64

	// 현재 트랜잭션의 갱신 수는 통계 flush와 무관하게 바로 보인다.
	err = tx.QueryRow(ctx, `
		SELECT pg_stat_get_xact_tuples_updated('public.youtube_live_sessions'::regclass),
		       pg_stat_get_xact_tuples_hot_updated('public.youtube_live_sessions'::regclass)
	`).Scan(&updated, &hot)
	require.NoError(t, err)
	require.EqualValues(t, refreshes, updated)
	require.EqualValues(t, refreshes, hot, "repeated observations must not rewrite live session indexes")
}
