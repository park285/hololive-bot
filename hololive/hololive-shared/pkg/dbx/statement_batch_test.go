package dbx

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type statementRecordingTx struct {
	Tx
	executed []string
	failAt   int
	failErr  error
}

func (t *statementRecordingTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	index := len(t.executed)
	t.executed = append(t.executed, sql)
	if index == t.failAt {
		return pgconn.CommandTag{}, t.failErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type statementRecordingBatchTx struct {
	*statementRecordingTx
	results          []*statementRecordingResults
	sizes            []int
	closeErr         error
	unclosedPrevious bool
	panicOnExec      bool
}

func (t *statementRecordingBatchTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	if len(t.results) > 0 && !t.results[len(t.results)-1].closed {
		t.unclosedPrevious = true
	}
	result := &statementRecordingResults{owner: t, ctx: ctx, batch: batch}
	t.results = append(t.results, result)
	t.sizes = append(t.sizes, batch.Len())
	return result
}

type statementRecordingResults struct {
	pgx.BatchResults
	owner  *statementRecordingBatchTx
	ctx    context.Context
	batch  *pgx.Batch
	next   int
	closed bool
}

func (r *statementRecordingResults) Exec() (pgconn.CommandTag, error) {
	if r.owner.panicOnExec {
		panic("test batch panic")
	}
	query := r.batch.QueuedQueries[r.next]
	r.next++
	return r.owner.Exec(r.ctx, query.SQL, query.Arguments...)
}

func (r *statementRecordingResults) Close() error {
	r.closed = true
	return r.owner.closeErr
}

func TestExecStatementsEmptyAndNil(t *testing.T) {
	if err := ExecStatements(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := ExecStatements(t.Context(), nil, []Statement{{SQL: "SELECT 1"}}); err == nil {
		t.Fatal("non-empty execution without a transaction must fail")
	}
}

func TestExecStatementsChunkingPreservesOrder(t *testing.T) {
	tx := &statementRecordingBatchTx{statementRecordingTx: &statementRecordingTx{failAt: -1}}
	statements := make([]Statement, 260)
	want := make([]string, len(statements))
	for i := range statements {
		want[i] = fmt.Sprintf("statement-%d", i)
		statements[i] = Statement{SQL: want[i], Operation: "test"}
	}
	if err := ExecStatements(t.Context(), tx, statements); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tx.executed, want) || !slices.Equal(tx.sizes, []int{128, 128, 4}) {
		t.Fatalf("batch order/sizes changed: sizes=%v executed=%d", tx.sizes, len(tx.executed))
	}
	if tx.unclosedPrevious {
		t.Fatal("a new batch started before closing its predecessor")
	}
	for _, result := range tx.results {
		if !result.closed {
			t.Fatal("batch result leaked")
		}
	}
}

func TestExecStatementsFailureClosesResultsAndJoinsErrors(t *testing.T) {
	executeFailure := errors.New("execute failure")
	closeFailure := errors.New("close failure")
	tx := &statementRecordingBatchTx{
		statementRecordingTx: &statementRecordingTx{failAt: 1, failErr: executeFailure},
		closeErr:             closeFailure,
	}
	statements := make([]Statement, 260)
	err := ExecStatements(t.Context(), tx, statements)
	if !errors.Is(err, executeFailure) || !errors.Is(err, closeFailure) {
		t.Fatalf("lost error cause: %v", err)
	}
	if len(tx.results) != 1 || !tx.results[0].closed || len(tx.executed) != 2 {
		t.Fatal("execution continued after error or did not close results")
	}
}

func TestExecStatementsSequentialFallback(t *testing.T) {
	failure := errors.New("fallback failure")
	tx := &statementRecordingTx{failAt: 1, failErr: failure}
	statements := []Statement{{SQL: "first"}, {SQL: "second"}, {SQL: "third"}}
	if err := ExecStatements(t.Context(), tx, statements); !errors.Is(err, failure) {
		t.Fatalf("fallback error=%v", err)
	}
	if !slices.Equal(tx.executed, []string{"first", "second"}) {
		t.Fatalf("fallback executed=%v", tx.executed)
	}
}

func TestExecStatementsCloseFailureIsReturned(t *testing.T) {
	failure := errors.New("close-only failure")
	tx := &statementRecordingBatchTx{
		statementRecordingTx: &statementRecordingTx{failAt: -1}, closeErr: failure,
	}
	if err := ExecStatements(t.Context(), tx, []Statement{{SQL: "first"}}); !errors.Is(err, failure) {
		t.Fatalf("close error was lost: %v", err)
	}
}

func TestExecStatementsPanicClosesResults(t *testing.T) {
	tx := &statementRecordingBatchTx{
		statementRecordingTx: &statementRecordingTx{failAt: -1}, panicOnExec: true,
	}
	defer func() {
		if recover() == nil {
			t.Error("execution panic was swallowed")
		}
		if len(tx.results) != 1 || !tx.results[0].closed {
			t.Error("panic leaked batch results")
		}
	}()
	if err := ExecStatements(t.Context(), tx, []Statement{{SQL: "first"}}); err != nil {
		t.Fatal(err)
	}
}

func TestExecStatementsCanceledContextDoesNotStartWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sequential := &statementRecordingTx{failAt: -1}
	batched := &statementRecordingBatchTx{statementRecordingTx: &statementRecordingTx{failAt: -1}}
	for _, tx := range []Tx{sequential, batched} {
		if err := ExecStatements(ctx, tx, []Statement{{SQL: "first"}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause was lost: %v", err)
		}
	}
	if len(sequential.executed) != 0 || len(batched.results) != 0 {
		t.Fatal("canceled execution must not dispatch statements")
	}
}

type nilStatementBatchTx struct{ Tx }

func (*nilStatementBatchTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func TestExecStatementsRejectsNilBatchResults(t *testing.T) {
	if err := ExecStatements(t.Context(), &nilStatementBatchTx{}, []Statement{{SQL: "first"}}); err == nil {
		t.Fatal("nil batch results must fail closed")
	}
}
