package migrationrunner

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
)

func TestInvalidIndexesDetectsAndDropsLeftoverInvalidIndex(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "CREATE TABLE inv_idx_target(x integer)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE INDEX idx_inv_leftover ON inv_idx_target(x)"); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE pg_index SET indisvalid = false WHERE indexrelid = 'idx_inv_leftover'::regclass"); err != nil {
		t.Fatalf("mark index invalid: %v", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()

	indexes, err := invalidIndexes(ctx, conn)
	if err != nil {
		t.Fatalf("invalidIndexes: %v", err)
	}
	if !slices.Contains(indexes, "public.idx_inv_leftover") {
		t.Fatalf("invalidIndexes = %v, want to contain public.idx_inv_leftover", indexes)
	}

	if err := dropInvalidIndexes(ctx, conn); err == nil {
		t.Fatal("dropInvalidIndexes must return a fail-loud error after dropping")
	}

	remaining, err := invalidIndexes(ctx, conn)
	if err != nil {
		t.Fatalf("invalidIndexes after drop: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining invalid indexes = %v, want none", remaining)
	}
}

func TestInvalidIndexesExcludesInProgressConcurrentBuild(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "CREATE TABLE inv_idx_progress(x integer)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO inv_idx_progress SELECT generate_series(1, 1000)"); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	// REPEATABLE READ 스냅샷을 쥔 tx가 살아있는 동안 CREATE INDEX CONCURRENTLY는
	// 'waiting for old snapshots' 단계에서 결정적으로 멈춘다 — 이 사이 pg_index에는
	// indisvalid=false 행이 존재하고 pg_stat_progress_create_index에 진행 행이 잡힌다.
	snapshotTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin snapshot tx: %v", err)
	}
	defer func() {
		if rollbackErr := snapshotTx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback snapshot tx: %v", rollbackErr)
		}
	}()
	if _, err := snapshotTx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		t.Fatalf("set isolation: %v", err)
	}
	if _, err := snapshotTx.Exec(ctx, "SELECT count(*) FROM inv_idx_progress"); err != nil {
		t.Fatalf("hold snapshot: %v", err)
	}

	buildDone := make(chan error, 1)
	go func() {
		_, buildErr := pool.Exec(context.Background(), "CREATE INDEX CONCURRENTLY idx_inv_inprogress ON inv_idx_progress(x)")
		buildDone <- buildErr
	}()

	waitForCreateIndexProgress(t, ctx, pool, "idx_inv_inprogress")

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	indexes, err := invalidIndexes(ctx, conn)
	conn.Release()
	if err != nil {
		t.Fatalf("invalidIndexes: %v", err)
	}
	if slices.Contains(indexes, "public.idx_inv_inprogress") {
		t.Fatal("진행 중인 CONCURRENTLY 빌드는 invalid-index 청소 대상에서 제외되어야 한다 (동시 빌드를 DROP하면 정상 빌드가 파괴된다)")
	}

	if err := snapshotTx.Rollback(ctx); err != nil {
		t.Fatalf("release snapshot: %v", err)
	}
	if err := <-buildDone; err != nil {
		t.Fatalf("concurrent index build: %v", err)
	}
}

func waitForCreateIndexProgress(t *testing.T, ctx context.Context, pool *pgxpool.Pool, indexName string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_progress_create_index p
				JOIN pg_class c ON c.oid = p.index_relid
				WHERE c.relname = $1
			)`, indexName).Scan(&present)
		if err != nil {
			t.Fatalf("poll create index progress: %v", err)
		}
		if present {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pg_stat_progress_create_index row")
}
