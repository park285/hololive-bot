package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLiveDecisionStatementsPreserveSessionHeadOrder(t *testing.T) {
	statements := liveDecisionStatements(nil, []live.SessionState{
		{VideoID: "a", ChannelID: "channel", Status: domain.LiveStatusLive, LifecycleOrigin: live.OriginObserved},
		{VideoID: "b", Status: domain.LiveStatusLive, HeadPresent: true},
		{VideoID: "c", ChannelID: "channel", Status: domain.LiveStatusLive, LifecycleOrigin: live.OriginObserved},
	})
	if len(statements) != 5 {
		t.Fatalf("statements=%d want 5", len(statements))
	}

	wantIDs := []string{"a", "a", "b", "c", "c"}
	wantOperations := []string{"insert live session", "upsert live head", "upsert live head", "insert live session", "upsert live head"}

	for i := range statements {
		if statements[i].Args[0] != wantIDs[i] || statements[i].Operation != wantOperations[i] {
			t.Fatalf("statement %d changed execution order", i)
		}
	}
}

// 세션 저장은 잠금 안에서 읽은 행과 reducer 결과를 비교해 바뀐 행만 reducer 값 그대로 쓴다.
// 최초공개 분류 저장은 분류만 보완하고 수명·메타데이터를 바꾸지 않는다.
func TestLiveSessionWriteFollowsLockedRow(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := live.SessionState{
		VideoID: "hotpath-noop", ChannelID: "hotpath-channel", Status: domain.LiveStatusLive,
		Title: "title", ScheduledStartTime: new(now.Add(-time.Hour)), StartedAt: new(now),
		LiveFirstSeenAt: new(now), LastSeenAt: now, LifecycleOrigin: live.OriginMetadataOnly,
	}

	persistLockedLiveSession(t, pool, &session)

	inserted := liveSessionXmin(t, pool, session.VideoID)

	persistLockedLiveSession(t, pool, &session)
	require.Equal(t, inserted, liveSessionXmin(t, pool, session.VideoID), "unchanged session must not be rewritten")

	// reducer가 정한 값은 SQL이 다시 병합하지 않고 그대로 저장한다.
	session.ScheduledStartTime, session.LastSeenAt = nil, now.Add(-time.Hour)
	persistLockedLiveSession(t, pool, &session)
	require.NotEqual(t, inserted, liveSessionXmin(t, pool, session.VideoID), "changed session must be written")

	var (
		scheduled *time.Time
		seen      time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, "SELECT scheduled_start_time, last_seen_at FROM youtube_live_sessions WHERE video_id=$1",
		session.VideoID).Scan(&scheduled, &seen))
	require.Nil(t, scheduled)
	require.True(t, seen.Equal(now.Add(-time.Hour)))

	classification := session

	classification.IsPremiere = new(true)
	classification.Status, classification.Title = domain.LiveStatusEnded, "classification must not replace title"

	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return upsertConfirmedPremiereSession(ctx, tx, &classification)
	}))

	var (
		status, title string
		premiere      bool
	)

	require.NoError(t, pool.QueryRow(ctx, "SELECT status, title, is_premiere FROM youtube_live_sessions WHERE video_id=$1",
		session.VideoID).Scan(&status, &title, &premiere))
	require.Equal(t, "LIVE", status, "Premiere classification overwrote lifecycle")
	require.Equal(t, "title", title, "Premiere classification overwrote metadata")
	require.True(t, premiere)
}

// persistLockedLiveSession은 consumer처럼 세션 행을 잠그고 읽은 뒤 next를 저장한다.
func persistLockedLiveSession(t *testing.T, pool *pgxpool.Pool, next *live.SessionState) {
	t.Helper()

	require.NoError(t, dbx.InPgxTx(t.Context(), pool, func(tx dbx.Tx) error {
		state := live.State{Sessions: map[string]live.SessionState{}}
		if err := loadLiveSessions(t.Context(), tx, &state, nil, []string{next.VideoID}); err != nil {
			return err
		}

		return persistLiveSessions(t.Context(), tx, state.Sessions, []live.SessionState{*next})
	}))
}

// liveSessionXmin은 세션 행을 마지막으로 쓴 트랜잭션 id다. 값이 그대로면 행을 다시 쓰지 않은 것이다.
func liveSessionXmin(t *testing.T, pool *pgxpool.Pool, videoID string) string {
	t.Helper()

	var xmin string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT xmin::text FROM youtube_live_sessions WHERE video_id=$1", videoID).Scan(&xmin))

	return xmin
}

// insertLiveSessionRow는 head 없이 세션 행만 새로 저장한다.
func insertLiveSessionRow(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	statement, _ := liveSessionWrite(nil, session)

	return execLiveSessionStatements(ctx, tx, []dbx.Statement{statement})
}

// 같은 근거를 다시 저장해 검토 snapshot을 무효화하지 않으며 새 사실 시각은 반드시 저장한다.
func TestLiveHeadNoopPreservesReviewSnapshotButNewEvidenceInvalidatesIt(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := live.SessionState{
		VideoID: "head-noop", ChannelID: "head-noop-channel", Status: domain.LiveStatusUpcoming,
		LifecycleOrigin: live.OriginObserved, LastSeenAt: now,
	}

	session.Clock.LastUpcomingPositiveAt = new(now)
	session.Clock.LastUpcomingPositiveSeenAt = new(now)

	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return insertLiveSessionRow(ctx, tx, &session)
	}); err != nil {
		t.Fatal(err)
	}

	assertLiveHeadWrites(t, pool, &session, 1)

	beforeTuple, beforeHash := liveHeadReviewSnapshot(t, pool, session.VideoID)

	assertLiveHeadWrites(t, pool, &session, 0)

	afterTuple, afterHash := liveHeadReviewSnapshot(t, pool, session.VideoID)
	if beforeTuple != afterTuple || beforeHash != afterHash {
		t.Fatal("identical head evidence rewrote tuple or invalidated review snapshot")
	}

	session.Clock.LastUpcomingPositiveSeenAt = new(now.Add(time.Second))

	assertLiveHeadWrites(t, pool, &session, 1)

	changedTuple, changedHash := liveHeadReviewSnapshot(t, pool, session.VideoID)
	if changedTuple == afterTuple || changedHash == afterHash {
		t.Fatal("new evidence clock did not persist and invalidate review snapshot")
	}

	assertLiveHeadWrites(t, pool, &session, 0)

	session.IgnoredAbsences = live.LoadedIgnoredAbsences([]time.Time{now.Add(-time.Minute)})

	assertLiveHeadWrites(t, pool, &session, 1)

	ignoredTuple, ignoredHash := liveHeadReviewSnapshot(t, pool, session.VideoID)
	if ignoredHash == changedHash {
		t.Fatal("changed replay evidence did not invalidate review snapshot")
	}

	assertLiveHeadWrites(t, pool, &session, 0)

	// 이력을 적재하지 않은 저장은 배열을 지우지 않고 검토 snapshot도 무효화하지 않는다.
	session.IgnoredAbsences = live.IgnoredAbsenceHistory{}

	assertLiveHeadWrites(t, pool, &session, 0)

	if tuple, hash := liveHeadReviewSnapshot(t, pool, session.VideoID); tuple != ignoredTuple || hash != ignoredHash {
		t.Fatal("omitted history rewrote the head or invalidated review snapshot")
	}

	session.IgnoredAbsences = live.LoadedIgnoredAbsences(nil)

	assertLiveHeadWrites(t, pool, &session, 1)

	session.IgnoredAbsences = live.LoadedIgnoredAbsences([]time.Time{})

	assertLiveHeadWrites(t, pool, &session, 0)

	session.IgnoredAbsences = live.IgnoredAbsenceHistory{}

	assertLiveHeadWrites(t, pool, &session, 0)
}

func assertLiveHeadWrites(t *testing.T, pool *pgxpool.Pool, session *live.SessionState, want int64) {
	t.Helper()

	statement := liveHeadStatement(session)

	tag, err := pool.Exec(t.Context(), statement.SQL, statement.Args...)
	if err != nil {
		t.Fatal(err)
	}

	if tag.RowsAffected() != want {
		t.Fatalf("head writes = %d, want %d", tag.RowsAffected(), want)
	}
}

func liveHeadReviewSnapshot(t *testing.T, pool *pgxpool.Pool, videoID string) (string, string) {
	t.Helper()

	var tuple, hash string

	if err := pool.QueryRow(t.Context(), `
		SELECT head.ctid::text, review.snapshot_sha256
		FROM youtube_live_reconciliation_heads head
		CROSS JOIN LATERAL youtube_live_review_snapshot(head.video_id) review
		WHERE head.video_id=$1`, videoID).Scan(&tuple, &hash); err != nil {
		t.Fatal(err)
	}

	return tuple, hash
}

// 읽은 행과 지금 행이 다르면 저장은 어떤 행에도 적용되지 않고 영상 단위 충돌로 보고된다.
// 행이 없다고 읽은 영상을 다른 writer가 먼저 만든 경우도 같다. 바뀌지 않은 NULL 제목은 그대로 둔다.
func TestLiveSessionWriteReportsRowChangedAfterRead(t *testing.T) {
	const videoID = "changed-after-read"

	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at, lifecycle_origin)
		VALUES ($1, 'changed-channel', 'UPCOMING', NULL, $2, 'metadata_only')`, videoID, now)
	require.NoError(t, err)

	readSessions := func() map[string]live.SessionState {
		state := live.State{Sessions: map[string]live.SessionState{}}

		require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
			return loadLiveSessions(ctx, tx, &state, nil, []string{videoID})
		}))

		return state.Sessions
	}

	persist := func(locked map[string]live.SessionState, write *live.SessionState) error {
		return dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
			return persistLiveSessions(ctx, tx, locked, []live.SessionState{*write})
		})
	}

	stored := readSessions()
	next := stored[videoID]

	next.LastSeenAt = now.Add(time.Minute)
	require.NoError(t, persist(stored, &next))

	var title *string

	require.NoError(t, pool.QueryRow(ctx, "SELECT title FROM youtube_live_sessions WHERE video_id=$1", videoID).Scan(&title))
	require.Nil(t, title, "unchanged NULL title must keep its stored representation")

	stale := readSessions()

	_, err = pool.Exec(ctx, "UPDATE youtube_live_sessions SET status='ENDED' WHERE video_id=$1", videoID)
	require.NoError(t, err)

	next = stale[videoID]
	next.Status = domain.LiveStatusLive

	for name, locked := range map[string]map[string]live.SessionState{"update": stale, "insert": nil} {
		err = persist(locked, &next)

		conflict, ok := errors.AsType[*LiveSessionConflictError](err)
		require.True(t, ok, "%s: error = %v", name, err)
		require.Equal(t, videoID, conflict.VideoID, name)
	}

	var status string

	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM youtube_live_sessions WHERE video_id=$1", videoID).Scan(&status))
	require.Equal(t, "ENDED", status, "conflicting write must not change the row")
}

func TestLivePendingUpsertSkipsIdenticalEvidence(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	args := []any{"hotpath-pending", "hotpath-channel", "EXPLICIT_END", int64(9000001), now, now, now, nil, true, true}

	for step, want := range []int64{1, 0, 1} {
		if step == 2 {
			args[7] = now
		}

		tag, err := pool.Exec(ctx, mustSQL("repository_live_pending_end_upsert.sql"), args...)
		if err != nil {
			t.Fatal(err)
		}

		if tag.RowsAffected() != want {
			t.Fatalf("step %d updated %d rows want %d", step, tag.RowsAffected(), want)
		}
	}
}

func TestLiveStatementBatchFailureRollsBack(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "CREATE TABLE hotpath_batch_regression (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return dbx.ExecStatements(ctx, tx, []dbx.Statement{
			{SQL: "INSERT INTO hotpath_batch_regression VALUES ($1)", Args: []any{1}},
			{SQL: "INSERT INTO hotpath_batch_regression VALUES ($1)", Args: []any{1}},
			{SQL: "INSERT INTO hotpath_batch_regression VALUES ($1)", Args: []any{2}},
		})
	})

	if databaseError, ok := errors.AsType[*pgconn.PgError](err); !ok || databaseError.Code != "23505" {
		t.Fatalf("expected PostgreSQL unique violation, got %v", err)
	}

	var count int

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM hotpath_batch_regression").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("failed transaction persisted %d rows", count)
	}
}

type hotpathRecordingTx struct {
	dbx.Tx

	ids     []string
	failAt  int
	failure error
}

func (tx *hotpathRecordingTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if len(args) == 0 {
		return pgconn.CommandTag{}, errors.New("missing first argument")
	}

	id, ok := args[0].(string)
	if !ok {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected first argument %T", args[0])
	}

	tx.ids = append(tx.ids, id)
	if len(tx.ids)-1 == tx.failAt {
		return pgconn.CommandTag{}, tx.failure
	}

	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestLiveSessionChunksKeepOrderAndStopAfterFailure(t *testing.T) {
	sessions := make([]live.SessionState, 130)
	for i := range sessions {
		sessions[i] = live.SessionState{VideoID: fmt.Sprintf("video-%03d", i), ChannelID: "channel", Status: domain.LiveStatusLive, LifecycleOrigin: live.OriginObserved}
	}

	failure := errors.New("second chunk failed")

	for _, failAt := range []int{-1, 129} {
		tx := &hotpathRecordingTx{failAt: failAt, failure: failure}
		err := persistLiveSessions(t.Context(), tx, nil, sessions)
		want := 2 * len(sessions)

		if failAt >= 0 {
			want = failAt + 1

			if !errors.Is(err, failure) {
				t.Fatalf("lost execution error: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}

		if len(tx.ids) != want {
			t.Fatalf("executed=%d want=%d", len(tx.ids), want)
		}

		for i, id := range tx.ids {
			if id != sessions[i/2].VideoID {
				t.Fatalf("session/head order changed at %d", i)
			}
		}
	}
}

func TestPendingLiveEndChunksKeepOrderAndStopAfterFailure(t *testing.T) {
	pending := make([]live.PendingEnd, 260)
	for i := range pending {
		pending[i].VideoID = fmt.Sprintf("video-%03d", i)
	}

	failure := errors.New("pending chunk failed")

	for _, failAt := range []int{-1, 130} {
		tx := &hotpathRecordingTx{failAt: failAt, failure: failure}
		err := persistPendingLiveEnds(t.Context(), tx, pending)
		want := len(pending)

		if failAt >= 0 {
			want = failAt + 1

			if !errors.Is(err, failure) {
				t.Fatalf("lost execution error: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}

		if len(tx.ids) != want {
			t.Fatalf("executed=%d want=%d", len(tx.ids), want)
		}

		for i, id := range tx.ids {
			if id != pending[i].VideoID {
				t.Fatalf("pending order changed at %d", i)
			}
		}
	}
}
