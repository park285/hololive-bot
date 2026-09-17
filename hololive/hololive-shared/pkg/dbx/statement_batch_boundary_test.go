package dbx

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type cancelAfterBatchTx struct {
	*statementRecordingBatchTx

	cancel context.CancelFunc
}

func (tx *cancelAfterBatchTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return &cancelAfterBatchResults{
		BatchResults: tx.statementRecordingBatchTx.SendBatch(ctx, batch),
		cancel:       tx.cancel,
	}
}

type cancelAfterBatchResults struct {
	pgx.BatchResults

	cancel context.CancelFunc
}

func (r *cancelAfterBatchResults) Close() error {
	defer r.cancel()

	if err := r.BatchResults.Close(); err != nil {
		return fmt.Errorf("close cancellation fixture batch: %w", err)
	}

	return nil
}

func TestExecStatementsCancellationBetweenBatchesStopsDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	recording := &statementRecordingBatchTx{
		statementRecordingTx: &statementRecordingTx{failAt: -1},
	}
	tx := &cancelAfterBatchTx{statementRecordingBatchTx: recording, cancel: cancel}
	statements := make([]Statement, maxHotpathBatchStatements+1)

	for i := range statements {
		statements[i] = Statement{SQL: "SELECT 1", Operation: "cancellation boundary"}
	}

	if err := ExecStatements(ctx, tx, statements); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation between batches was lost: %v", err)
	}

	if len(recording.results) != 1 || len(recording.executed) != maxHotpathBatchStatements {
		t.Fatalf("dispatched after cancellation: batches=%d statements=%d", len(recording.results), len(recording.executed))
	}

	if !recording.results[0].closed {
		t.Fatal("cancellation left the first batch open")
	}
}

type cancelAfterStatementTx struct {
	*statementRecordingTx

	cancel context.CancelFunc
}

func (tx *cancelAfterStatementTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.statementRecordingTx.Exec(ctx, sql, args...)
	tx.cancel()

	if err != nil {
		return tag, fmt.Errorf("execute cancellation fixture statement: %w", err)
	}

	return tag, nil
}

func TestExecStatementsCancellationBetweenSequentialStatementsStopsDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	tx := &cancelAfterStatementTx{
		statementRecordingTx: &statementRecordingTx{failAt: -1},
		cancel:               cancel,
	}
	statements := []Statement{{SQL: testStatementFirstSQL}, {SQL: testStatementSecondSQL}}

	if err := ExecStatements(ctx, tx, statements); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation between statements was lost: %v", err)
	}

	if len(tx.executed) != 1 || tx.executed[0] != testStatementFirstSQL {
		t.Fatalf("sequential execution continued after cancellation: %v", tx.executed)
	}
}
