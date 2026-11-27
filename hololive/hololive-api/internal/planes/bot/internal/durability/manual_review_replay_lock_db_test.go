package durability

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManualReviewReplayChecksCutoffAfterWriterWait(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	start := time.Now()

	var (
		clock      atomic.Int64
		clockReads atomic.Int32
	)

	clock.Store(start.UnixMilli())

	store.now = func() time.Time {
		clockReads.Add(1)

		return time.UnixMilli(clock.Load())
	}

	repo := newTestReplyOutboxRepository(store)
	id := seedManualReviewCreatedAt(ctx, t, store, repo, "message:replay-lock-cutoff", start.Add(-ManualReplayCutoff+2*time.Second))

	release, holderDone := holdWriter(ctx, store)
	readsWhileHeld := clockReads.Load()

	type replayResult struct {
		outcome ManualReviewOutcome
		err     error
	}

	done := make(chan replayResult, 1)

	go func() {
		outcome, replayErr := store.GrantManualReplay(ctx, id, testOperatorEmail, manualReplayReason)
		done <- replayResult{outcome: outcome, err: replayErr}
	}()

	require.Never(t, func() bool { return clockReads.Load() > readsWhileHeld }, 200*time.Millisecond, 5*time.Millisecond,
		"the replay must read the clock only after it owns the writer")

	clock.Store(start.Add(3 * time.Second).UnixMilli())
	release()
	require.NoError(t, <-holderDone)

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, ManualReviewCutoffExpired, result.outcome)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	var auditCount int

	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = ?1`, id).Scan(&auditCount))
	require.Zero(t, auditCount)
	assertRejectedReplayStaysUnclaimable(ctx, t, store, repo, id)
}

func seedManualReviewCreatedAt(ctx context.Context, t *testing.T, store *Store, repo *ReplyOutboxRepository, messageID string, createdAt time.Time) int64 {
	t.Helper()

	entry := newReplyOutboxEntry(messageID, 0, `{"body":"stored"}`)
	_, err := repo.Insert(ctx, entry)
	require.NoError(t, err)

	var id int64

	require.NoError(t, store.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'manual_review', attempts = 5, first_attempt_at = ?2, created_at = ?3
		WHERE message_id = ?1 RETURNING id`, messageID, createdAt.Add(-time.Hour).UnixMilli(), createdAt.UnixMilli()).Scan(&id))

	return id
}

func holdWriter(ctx context.Context, store *Store) (func(), <-chan error) {
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)

	go func() {
		done <- store.write(ctx, func(*sql.Tx, int64) error {
			close(held)
			<-release

			return nil
		})
	}()

	<-held

	return func() { close(release) }, done
}
