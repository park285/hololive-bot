package dispatchops

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type rollbackRecorder struct {
	pgx.Tx

	err         error
	ctxErr      error
	hasDeadline bool
}

func (r *rollbackRecorder) Rollback(ctx context.Context) error {
	r.ctxErr = ctx.Err()
	_, r.hasDeadline = ctx.Deadline()

	return r.err
}

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

// 취소된 요청에서도 rollback은 취소와 분리된 제한 시간 안에서 실행되고, 실패 원인을 로그에 남긴다.
func TestRollbackRunsDetachedFromCanceledRequestAndLogsCause(t *testing.T) {
	logs := captureDefaultLog(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	tx := &rollbackRecorder{err: errors.New("connection reset")}
	rollback(ctx, tx)

	if tx.ctxErr != nil || !tx.hasDeadline {
		t.Fatalf("rollback ctx err = %v, deadline = %v; want live ctx with deadline", tx.ctxErr, tx.hasDeadline)
	}

	if !strings.Contains(logs.String(), "dispatch operation rollback failed") || !strings.Contains(logs.String(), "connection reset") {
		t.Fatalf("rollback log = %q, want failure message with cause", logs.String())
	}
}

func TestRollbackIgnoresClosedTransaction(t *testing.T) {
	logs := captureDefaultLog(t)

	rollback(t.Context(), &rollbackRecorder{err: pgx.ErrTxClosed})

	if logs.Len() != 0 {
		t.Fatalf("rollback log = %q, want no log for closed transaction", logs.String())
	}
}
