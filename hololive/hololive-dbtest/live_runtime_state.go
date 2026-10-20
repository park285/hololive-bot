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

// LiveRuntimeStateReader는 수명 모델 오라클을 실행할 pool·연결·트랜잭션이다.
type LiveRuntimeStateReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// LiveRuntimeStateWriter는 수명 모델을 재계산할 pool·연결·트랜잭션이다.
type LiveRuntimeStateWriter interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// LiveRuntimeStateDrift는 운영 점검과 같은 오라클 SQL로 youtube_live_runtime_state의 누락·잉여·값 변경 행 수를 센다.
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

// RefreshAllLiveRuntimeState는 원본을 직접 쓴 fixture 뒤 기존 모델·session·head의 모든 key로 수명 모델을 재계산한다.
// 운영 writer는 변경 key만 갱신하므로 이 함수는 시험 seed 전용이며 READ COMMITTED에서만 성공한다.
func RefreshAllLiveRuntimeState(tb testing.TB, db LiveRuntimeStateWriter) {
	tb.Helper()

	if _, err := db.Exec(tb.Context(), `SELECT refresh_youtube_live_runtime_state(ARRAY(
		SELECT video_id FROM youtube_live_runtime_state
		UNION SELECT video_id FROM youtube_live_sessions
		UNION SELECT video_id FROM youtube_live_reconciliation_heads))`); err != nil {
		tb.Fatalf("refresh all live runtime state: %v", err)
	}
}
