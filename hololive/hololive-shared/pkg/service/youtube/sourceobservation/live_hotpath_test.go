package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestLiveDecisionStatementsPreserveSessionHeadOrder(t *testing.T) {
	statements := liveDecisionStatements([]live.SessionState{
		{VideoID: "a", ChannelID: "channel", Status: live.StatusLive},
		{VideoID: "b", Status: live.StatusLive},
		{VideoID: "c", ChannelID: "channel", Status: live.StatusLive},
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
	if len(statements[0].Args) != 13 || len(statements[1].Args) != 19 || statements[0].Args[12] != false {
		t.Fatal("SQL argument shape changed")
	}
}

func TestLiveSessionUpsertSkipsUnchangedEffectiveValues(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduled := now.Add(-time.Hour)
	args := []any{"hotpath-noop", "hotpath-channel", "LIVE", "title", "", "", scheduled, now, nil, now, now, nil, false}
	query := mustSQL("repository_live_session_upsert_0047_47.sql")
	for step, want := range []int64{1, 0, 1, 1, 0} {
		switch step {
		case 1:
			args[6], args[10] = nil, now.Add(-time.Hour)
		case 2:
			args[10] = now.Add(time.Minute)
		case 3:
			args[11], args[12] = true, true
		}
		tag, err := pool.Exec(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		if tag.RowsAffected() != want {
			t.Fatalf("step %d updated %d rows want %d", step, tag.RowsAffected(), want)
		}
	}
	var gotScheduled, gotSeen time.Time
	if err := pool.QueryRow(ctx, "SELECT scheduled_start_time, last_seen_at FROM youtube_live_sessions WHERE video_id=$1", args[0]).Scan(&gotScheduled, &gotSeen); err != nil {
		t.Fatal(err)
	}
	if !gotScheduled.Equal(scheduled) || !gotSeen.Equal(now.Add(time.Minute)) {
		t.Fatal("no-op guard changed effective persisted values")
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
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
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
		sessions[i] = live.SessionState{VideoID: fmt.Sprintf("video-%03d", i), ChannelID: "channel", Status: live.StatusLive}
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
