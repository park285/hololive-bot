package dbtest

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const liveRuntimeStateDriftQuery = "scripts/maintenance/queries/live_runtime_state_drift.sql"

type LiveRuntimeStateReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type LiveRuntimeStateWriter interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func LiveRuntimeStateDrift(tb testing.TB, db LiveRuntimeStateReader) [3]int64 {
	tb.Helper()

	root, err := findRepoRoot()
	if err != nil {
		tb.Fatalf("find repo root for live runtime drift: %v", err)
	}

	raw, err := fs.ReadFile(os.DirFS(root), liveRuntimeStateDriftQuery)
	if err != nil {
		tb.Fatalf("read live runtime drift query: %v", err)
	}

	var drift [3]int64

	if err := db.QueryRow(tb.Context(), string(raw)).Scan(&drift[0], &drift[1], &drift[2]); err != nil {
		tb.Fatalf("query live runtime drift: %v", err)
	}

	return drift
}

func RefreshAllLiveRuntimeState(tb testing.TB, db LiveRuntimeStateWriter) {
	tb.Helper()
	RefreshAllLiveRuntimeStateContext(tb.Context(), tb, db)
}

func RefreshAllLiveRuntimeStateContext(ctx context.Context, tb testing.TB, db LiveRuntimeStateWriter) {
	tb.Helper()

	if _, err := db.Exec(ctx, `SELECT refresh_youtube_live_runtime_state(ARRAY(
		SELECT video_id FROM youtube_live_runtime_state
		UNION SELECT video_id FROM youtube_live_sessions
		UNION SELECT video_id FROM youtube_live_reconciliation_heads))`); err != nil {
		tb.Fatalf("refresh all live runtime state: %v", err)
	}
}
