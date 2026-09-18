package sourceobservation

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestLiveStatementBatchFailureRollsBackEarlierChunks(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "CREATE TABLE hotpath_chunk_rollback (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	// 64세션의 session/head 128문장이 성공한 뒤 다음 전송에서 실패시킨다.
	const firstChunkStatements = 2 * liveSessionBatchSize

	statements := make([]dbx.Statement, 2*firstChunkStatements+1)

	for i := range statements {
		statements[i] = dbx.Statement{
			SQL:       "INSERT INTO hotpath_chunk_rollback VALUES ($1)",
			Args:      []any{i},
			Operation: "insert rollback fixture",
		}
	}

	statements[firstChunkStatements].Args[0] = 0

	err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return dbx.ExecStatements(ctx, tx, statements)
	})

	if databaseError, ok := errors.AsType[*pgconn.PgError](err); !ok || databaseError.Code != "23505" {
		t.Fatalf("expected second-batch unique violation, got %v", err)
	}

	var count int

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM hotpath_chunk_rollback").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("earlier batch escaped transaction rollback: %d rows", count)
	}

	// 실패 후 정상 트랜잭션이 같은 입력 전체를 기록할 수 있어야 한다.
	statements[firstChunkStatements].Args[0] = firstChunkStatements

	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return dbx.ExecStatements(ctx, tx, statements)
	}); err != nil {
		t.Fatalf("valid transaction after rollback failed: %v", err)
	}

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM hotpath_chunk_rollback").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != len(statements) {
		t.Fatalf("successful retry wrote %d rows, want %d", count, len(statements))
	}
}

func TestLiveStatementBatchesPreserveDeferredForeignKeyBoundary(t *testing.T) {
	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()
	// SendBatch의 사전 prepare와 혼동하지 않도록 DDL은 별도 실행한다.
	if _, err := pool.Exec(ctx, "CREATE TABLE hotpath_batch_parent (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE hotpath_batch_child (
		id integer PRIMARY KEY,
		parent_id integer NOT NULL REFERENCES hotpath_batch_parent(id) DEFERRABLE INITIALLY DEFERRED
	)`); err != nil {
		t.Fatal(err)
	}

	const children = 2 * liveSessionBatchSize

	statements := make([]dbx.Statement, 0, children+1)

	for i := range children {
		statements = append(statements, dbx.Statement{
			SQL:       "INSERT INTO hotpath_batch_child VALUES ($1, $2)",
			Args:      []any{i, 1},
			Operation: "insert deferred child fixture",
		})
	}

	// 참조 대상은 다음 전송에 있다. 전송 경계가 커밋 경계가 되어서는 안 된다.
	statements = append(statements, dbx.Statement{
		SQL:       "INSERT INTO hotpath_batch_parent VALUES ($1)",
		Args:      []any{1},
		Operation: "insert deferred parent fixture",
	})

	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return dbx.ExecStatements(ctx, tx, statements)
	}); err != nil {
		t.Fatalf("deferred constraint was checked before transaction commit: %v", err)
	}

	var count int

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM hotpath_batch_child").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != children {
		t.Fatalf("persisted children=%d want=%d", count, children)
	}
}
