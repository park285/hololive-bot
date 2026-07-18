package migrationrunner

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-dbtest"
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

	indexes, err := invalidIndexes(ctx, conn, nil)
	if err != nil {
		t.Fatalf("invalidIndexes: %v", err)
	}
	if !slices.Contains(indexes, "public.idx_inv_leftover") {
		t.Fatalf("invalidIndexes = %v, want to contain public.idx_inv_leftover", indexes)
	}

	targets, unparsed := concurrentIndexTargets("CREATE INDEX CONCURRENTLY idx_inv_leftover ON inv_idx_target(x)")
	if unparsed {
		t.Fatal("concurrent index target was not parsed")
	}
	if err := dropInvalidIndexes(ctx, conn, targets); err == nil {
		t.Fatal("dropInvalidIndexes must return a fail-loud error after dropping")
	}

	remaining, err := invalidIndexes(ctx, conn, nil)
	if err != nil {
		t.Fatalf("invalidIndexes after drop: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining invalid indexes = %v, want none", remaining)
	}
}

func TestFailedMigrationDropsOnlyItsOwnInvalidConcurrentIndex(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	for _, statement := range []string{
		"CREATE TABLE inv_idx_target_scope(x integer)",
		"CREATE TABLE inv_idx_unrelated_scope(x integer)",
		"CREATE INDEX idx_target_cleanup ON inv_idx_target_scope(x)",
		"CREATE INDEX idx_unrelated_cleanup ON inv_idx_unrelated_scope(x)",
		"UPDATE pg_index SET indisvalid = false WHERE indexrelid IN ('idx_target_cleanup'::regclass, 'idx_unrelated_cleanup'::regclass)",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed invalid indexes with %q: %v", statement, err)
		}
	}

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 target.sql\n")},
		"target.sql": {Data: []byte(
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_target_cleanup ON inv_idx_target_scope(x);\n" +
				"SELECT 1/0;\n",
		)},
	}
	if _, err := Run(ctx, pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want migration failure")
	}

	if indexExists(t, pool, "idx_target_cleanup") {
		t.Fatal("failed migration target invalid index still exists")
	}
	if !indexExists(t, pool, "idx_unrelated_cleanup") {
		t.Fatal("unrelated invalid index was dropped")
	}
}

func TestInvalidIndexCleanupUsesParentTableSchema(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	for _, statement := range []string{
		"CREATE SCHEMA audit",
		"CREATE TABLE audit.events(id integer)",
		"CREATE TABLE public.events(id integer)",
		"CREATE INDEX idx_schema_target ON audit.events(id)",
		"CREATE INDEX idx_schema_target ON public.events(id)",
		"UPDATE pg_index SET indisvalid = false WHERE indexrelid IN ('audit.idx_schema_target'::regclass, 'public.idx_schema_target'::regclass)",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed schema-scoped indexes with %q: %v", statement, err)
		}
	}

	targets, unparsed := concurrentIndexTargets("CREATE INDEX CONCURRENTLY idx_schema_target ON audit.events(id)")
	if unparsed {
		t.Fatal("schema-qualified parent table target was not parsed")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	if err := dropInvalidIndexes(ctx, conn, targets); err == nil {
		t.Fatal("dropInvalidIndexes must return a fail-loud error after dropping")
	}

	if indexExists(t, pool, "audit.idx_schema_target") {
		t.Fatal("invalid index on the migration target table still exists")
	}
	if !indexExists(t, pool, "public.idx_schema_target") {
		t.Fatal("same-named invalid index on another table was dropped")
	}
}

func TestInvalidIndexCleanupTreatsDotInQuotedNameAsIndexName(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	for _, statement := range []string{
		"CREATE SCHEMA audit",
		"CREATE SCHEMA a",
		"CREATE TABLE audit.events(id integer)",
		"CREATE TABLE a.events(id integer)",
		`CREATE INDEX "a.b" ON audit.events(id)`,
		"CREATE INDEX b ON a.events(id)",
		`UPDATE pg_index SET indisvalid = false WHERE indexrelid IN ('audit."a.b"'::regclass, 'a.b'::regclass)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed quoted-dot indexes with %q: %v", statement, err)
		}
	}

	targets, unparsed := concurrentIndexTargets(`CREATE INDEX CONCURRENTLY "a.b" ON audit.events(id)`)
	if unparsed {
		t.Fatal("quoted-dot index target was not parsed")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	if err := dropInvalidIndexes(ctx, conn, targets); err == nil {
		t.Fatal("dropInvalidIndexes must return a fail-loud error after dropping")
	}

	if indexExists(t, pool, `audit."a.b"`) {
		t.Fatal("quoted-dot invalid index on the migration target table still exists")
	}
	if !indexExists(t, pool, "a.b") {
		t.Fatal("similarly flattened invalid index on another table was dropped")
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
	indexes, err := invalidIndexes(ctx, conn, nil)
	if err != nil {
		conn.Release()
		t.Fatalf("invalidIndexes: %v", err)
	}
	if slices.Contains(indexes, "public.idx_inv_inprogress") {
		conn.Release()
		t.Fatal("진행 중인 CONCURRENTLY 빌드는 invalid-index 청소 대상에서 제외되어야 한다 (동시 빌드를 DROP하면 정상 빌드가 파괴된다)")
	}
	targets, unparsed := concurrentIndexTargets("CREATE INDEX CONCURRENTLY idx_inv_inprogress ON inv_idx_progress(x)")
	if unparsed {
		conn.Release()
		t.Fatal("concurrent index target was not parsed")
	}
	targeted, err := invalidIndexes(ctx, conn, targets)
	if err != nil {
		conn.Release()
		t.Fatalf("targeted invalidIndexes: %v", err)
	}
	if slices.Contains(targeted, "public.idx_inv_inprogress") {
		conn.Release()
		t.Fatal("진행 중인 target 이름도 named invalid-index 청소 대상에서 제외되어야 한다")
	}
	if err := dropInvalidIndexes(ctx, conn, targets); err != nil {
		conn.Release()
		t.Fatalf("targeted cleanup must leave in-progress build alone: %v", err)
	}
	conn.Release()

	if err := snapshotTx.Rollback(ctx); err != nil {
		t.Fatalf("release snapshot: %v", err)
	}
	if err := <-buildDone; err != nil {
		t.Fatalf("concurrent index build: %v", err)
	}
}

func TestConcurrentIndexNameExtractionIsTargetScoped(t *testing.T) {
	got := concurrentIndexNames(`
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_one ON a(id);
CREATE UNIQUE INDEX CONCURRENTLY "idx_two" ON "ops"."b"(id);
CREATE INDEX CONCURRENTLY idx_one ON c(id);
`)
	want := []string{"idx_one", "idx_two"}
	if !slices.Equal(got, want) {
		t.Fatalf("concurrentIndexNames() = %v, want %v", got, want)
	}
}

func TestConcurrentIndexNameExtractionFoldsOnlyUnquotedIdentifiers(t *testing.T) {
	got := concurrentIndexNames(`
CREATE INDEX CONCURRENTLY IDX_UPPER ON a(id);
CREATE INDEX CONCURRENTLY "IDX_Quoted" ON b(id);
CREATE INDEX CONCURRENTLY IDX_SCHEMA_UPPER ON OPS.c(id);
CREATE INDEX CONCURRENTLY "IDX_Schema_Quoted" ON "Ops".d(id);
`)
	want := []string{"idx_upper", "IDX_Quoted", "idx_schema_upper", "IDX_Schema_Quoted"}
	if !slices.Equal(got, want) {
		t.Fatalf("concurrentIndexNames() = %v, want %v", got, want)
	}
}

func TestConcurrentIndexNameExtractionFailsClosedWhenNameIsOmitted(t *testing.T) {
	names, unparsed := concurrentIndexTargets("CREATE INDEX CONCURRENTLY ON target_table(id);")
	if len(names) != 0 || !unparsed {
		t.Fatalf("concurrentIndexTargets() = (%v, %v), want ([], true)", names, unparsed)
	}
}

func TestConcurrentIndexNameExtractionFailsClosedForSchemaQualifiedIndexName(t *testing.T) {
	targets, unparsed := concurrentIndexTargets(`CREATE INDEX CONCURRENTLY "audit"."idx_events" ON audit.events(id);`)
	if len(targets) != 0 || !unparsed {
		t.Fatalf("concurrentIndexTargets() = (%v, %v), want ([], true)", targets, unparsed)
	}
}

func TestConcurrentIndexTargetPreservesParentRelationIdentity(t *testing.T) {
	targets, unparsed := concurrentIndexTargets(`
CREATE INDEX CONCURRENTLY IDX_EVENTS ON ONLY AUDIT.EVENTS(id);
CREATE INDEX CONCURRENTLY "IDX_Events" ON ONLY "Audit"."Events"(id);
`)
	want := []concurrentIndexTarget{
		{indexName: "idx_events", tableRelation: `"audit"."events"`},
		{indexName: "IDX_Events", tableRelation: `"Audit"."Events"`},
	}
	if !slices.Equal(targets, want) || unparsed {
		t.Fatalf("concurrentIndexTargets() = (%v, %v), want (%v, false)", targets, unparsed, want)
	}
}

func TestConcurrentIndexNameExtractionAllowsCommentsBetweenKeywords(t *testing.T) {
	names, unparsed := concurrentIndexTargets(`
CREATE /* create note */ INDEX -- index note
CONCURRENTLY /* target note */ IDX_COMMENTED ON target_table(id);
`)
	want := []concurrentIndexTarget{{indexName: "idx_commented", tableRelation: `"target_table"`}}
	if !slices.Equal(names, want) || unparsed {
		t.Fatalf("concurrentIndexTargets() = (%v, %v), want (%v, false)", names, unparsed, want)
	}
}

func TestConcurrentIndexNameExtractionIgnoresCommentsAndStringLiterals(t *testing.T) {
	got := concurrentIndexNames(`
-- CREATE INDEX CONCURRENTLY idx_commented ON ignored(id);
SELECT 'CREATE INDEX CONCURRENTLY idx_literal ON ignored(id)';
/* CREATE UNIQUE INDEX CONCURRENTLY idx_block_commented ON ignored(id); */
CREATE INDEX CONCURRENTLY idx_real ON actual(id);
`)
	want := []string{"idx_real"}
	if !slices.Equal(got, want) {
		t.Fatalf("concurrentIndexNames() = %v, want %v", got, want)
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

func indexExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatalf("check index %s: %v", name, err)
	}
	return exists
}
