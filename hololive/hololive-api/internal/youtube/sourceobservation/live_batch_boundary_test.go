package sourceobservation

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestLiveStatementBatchFailureRollsBackEarlierChunks(t *testing.T) {
	t.Parallel()

	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "CREATE TABLE hotpath_chunk_rollback (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

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
	t.Parallel()

	pool, _, _, _ := startLivePersist(t)
	ctx := t.Context()

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
