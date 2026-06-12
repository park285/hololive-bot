package dispatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type fakeExecOnlyDB struct {
	execCalls []string
}

func (f *fakeExecOnlyDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeExecOnlyDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("query not supported in fake")
}

func (f *fakeExecOnlyDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

type fakeBatchDB struct {
	fakeExecOnlyDB
	batches []*pgx.Batch
}

func (f *fakeBatchDB) SendBatch(_ context.Context, batch *pgx.Batch) pgx.BatchResults {
	f.batches = append(f.batches, batch)
	return &fakeBatchResults{remaining: batch.Len()}
}

type fakeBatchResults struct {
	remaining int
}

func (r *fakeBatchResults) Exec() (pgconn.CommandTag, error) {
	if r.remaining <= 0 {
		return pgconn.CommandTag{}, errors.New("no queued statement")
	}
	r.remaining--
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (r *fakeBatchResults) Query() (pgx.Rows, error) {
	return nil, errors.New("query not supported in fake")
}

func (r *fakeBatchResults) QueryRow() pgx.Row {
	return nil
}

func (r *fakeBatchResults) Close() error {
	return nil
}

func newBatchTestStatusUpdater(db any) *StatusUpdater {
	return newStatusUpdater(db, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
}

func batchTestTokens(count int) []outboxLockToken {
	lockedAt := time.Now().UTC().Add(-time.Minute)
	tokens := make([]outboxLockToken, 0, count)
	for i := range count {
		tokens = append(tokens, outboxLockToken{id: int64(i + 1), lockedAt: &lockedAt})
	}
	return tokens
}

// locked_at 조건이 row 마다 달라 IN-clause 로 합칠 수 없는 per-row UPDATE 를
// pgx.Batch 한 번의 round trip 으로 보내는지 검증한다.
func TestMarkSentBatchIfLockedUsesSingleBatchRoundTrip(t *testing.T) {
	t.Parallel()

	db := &fakeBatchDB{}
	updater := newBatchTestStatusUpdater(db)

	updater.markSentBatchIfLocked(context.Background(), batchTestTokens(3))

	require.Len(t, db.batches, 1, "expected one SendBatch round trip")
	require.Equal(t, 3, db.batches[0].Len(), "all live tokens must be queued in the batch")
	require.Empty(t, db.execCalls, "batch-capable db must not fall back to per-row Exec")
}

func TestMarkSentBatchIfLockedSkipsTokensWithoutLock(t *testing.T) {
	t.Parallel()

	db := &fakeBatchDB{}
	updater := newBatchTestStatusUpdater(db)

	lockedAt := time.Now().UTC()
	tokens := []outboxLockToken{
		{id: 0, lockedAt: &lockedAt},
		{id: 7, lockedAt: nil},
		{id: 11, lockedAt: &lockedAt},
	}
	updater.markSentBatchIfLocked(context.Background(), tokens)

	require.Len(t, db.batches, 1)
	require.Equal(t, 1, db.batches[0].Len(), "only tokens with id and lock are batched")
}

func TestMarkSentBatchIfLockedFallsBackToPerRowExecWithoutBatchSupport(t *testing.T) {
	t.Parallel()

	db := &fakeExecOnlyDB{}
	updater := newBatchTestStatusUpdater(db)

	updater.markSentBatchIfLocked(context.Background(), batchTestTokens(3))

	require.Len(t, db.execCalls, 3, "non-batch querier keeps the per-row path")
}
