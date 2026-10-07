package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLiveDecisionStatementsPreserveSessionHeadOrder(t *testing.T) {
	statements := liveDecisionStatements([]live.SessionState{
		{VideoID: "a", ChannelID: "channel", Status: domain.LiveStatusLive, LifecycleOrigin: live.OriginObserved},
		{VideoID: "b", Status: domain.LiveStatusLive, HeadPresent: true},
		{VideoID: "c", ChannelID: "channel", Status: domain.LiveStatusLive, LifecycleOrigin: live.OriginObserved},
	})
	if len(statements) != 5 {
		t.Fatalf("statements=%d want 5", len(statements))
	}

	wantIDs := []string{"a", "a", "b", "c", "c"}
	wantOperations := []string{"upsert live session", "upsert live head", "upsert live head", "upsert live session", "upsert live head"}

	for i := range statements {
		if statements[i].Args[0] != wantIDs[i] || statements[i].Operation != wantOperations[i] {
			t.Fatalf("statement %d changed execution order", i)
		}
	}
}

func TestLiveSessionUpsertSkipsUnchangedEffectiveValues(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduled := now.Add(-time.Hour)
	session := live.SessionState{
		VideoID: "hotpath-noop", ChannelID: "hotpath-channel", Status: domain.LiveStatusLive,
		Title: "title", ScheduledStartTime: new(scheduled), StartedAt: new(now),
		LiveFirstSeenAt: new(now), LastSeenAt: now, LifecycleOrigin: live.OriginObserved,
	}
	classificationOnly := false

	for step, want := range []int64{1, 0, 1, 1, 0} {
		switch step {
		case 1:
			session.ScheduledStartTime, session.LastSeenAt = nil, now.Add(-time.Hour)
		case 2:
			session.LastSeenAt = now.Add(time.Minute)
		case 3:
			session.IsPremiere, classificationOnly = new(true), true
			session.Status, session.Title = domain.LiveStatusEnded, "classification must not replace title"
			session.TitleObservedAt, session.LastSeenAt = new(now.Add(time.Hour)), now.Add(time.Hour)
		}

		statement := liveSessionStatement(&session, classificationOnly)

		tag, err := pool.Exec(ctx, statement.SQL, statement.Args...)
		if err != nil {
			t.Fatal(err)
		}

		if tag.RowsAffected() != want {
			t.Fatalf("step %d updated %d rows want %d", step, tag.RowsAffected(), want)
		}
	}

	var gotScheduled, gotSeen time.Time

	if err := pool.QueryRow(ctx, "SELECT scheduled_start_time, last_seen_at FROM youtube_live_sessions WHERE video_id=$1", session.VideoID).Scan(&gotScheduled, &gotSeen); err != nil {
		t.Fatal(err)
	}

	var status, title string

	if err := pool.QueryRow(ctx, "SELECT status,title FROM youtube_live_sessions WHERE video_id=$1", session.VideoID).Scan(&status, &title); err != nil {
		t.Fatal(err)
	}

	if status != "LIVE" || title != "title" {
		t.Fatal("Premiere classification overwrote lifecycle or metadata")
	}

	if !gotScheduled.Equal(scheduled) || !gotSeen.Equal(now.Add(time.Minute)) {
		t.Fatal("no-op guard changed effective persisted values")
	}
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
		return dbx.ExecStatements(ctx, tx, []dbx.Statement{liveSessionStatement(&session, false)})
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

func TestLiveSessionUpsertRejectsStaleOrUnprovenMetadata(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduled := now.Add(time.Hour)
	current := live.SessionState{
		VideoID: "metadata-clock", ChannelID: "metadata-channel", Status: domain.LiveStatusUpcoming,
		Title: "Current title", TitleObservedAt: new(now),
		ScheduledStartTime: new(scheduled), ScheduleObservedAt: new(now), LastSeenAt: now,
		LifecycleOrigin: live.OriginObserved,
	}

	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return dbx.ExecStatements(ctx, tx, []dbx.Statement{liveSessionStatement(&current, false)})
	}); err != nil {
		t.Fatal(err)
	}

	for _, clock := range []*time.Time{new(now.Add(-time.Minute)), new(now), nil} {
		incoming := current

		incoming.Title = "Conflicting title"
		incoming.ScheduledStartTime = new(scheduled.Add(time.Hour))
		incoming.TitleObservedAt, incoming.ScheduleObservedAt = clock, clock
		incoming.LastSeenAt = now.Add(time.Minute)

		if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
			return dbx.ExecStatements(ctx, tx, []dbx.Statement{liveSessionStatement(&incoming, false)})
		}); err != nil {
			t.Fatal(err)
		}
	}

	var title string

	var start, titleClock, scheduleClock time.Time

	if err := pool.QueryRow(ctx, `
		SELECT title,scheduled_start_time,title_observed_at,schedule_observed_at
		FROM youtube_live_sessions WHERE video_id=$1
	`, current.VideoID).Scan(&title, &start, &titleClock, &scheduleClock); err != nil {
		t.Fatal(err)
	}

	if title != current.Title || !start.Equal(scheduled) || !titleClock.Equal(now) || !scheduleClock.Equal(now) {
		t.Fatalf("stale snapshot replaced metadata: %q %s clocks=%s/%s", title, start, titleClock, scheduleClock)
	}
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
		err := persistLiveSessions(t.Context(), tx, sessions)
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
