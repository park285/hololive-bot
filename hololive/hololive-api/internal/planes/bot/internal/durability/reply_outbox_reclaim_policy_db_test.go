package durability

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReplyReclaimPreservesSafetyGrants(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()
	cases := []struct {
		name, want       string
		attempts, grants int32
		age              time.Duration
	}{
		{name: "retry", attempts: 4, age: time.Hour, want: "pending"},
		{name: "exhausted", attempts: 5, age: time.Hour, want: ReplyOutboxManualReview},
		{name: "grant", attempts: 5, grants: 1, age: time.Hour, want: "pending"},
		{name: "horizon", attempts: 1, age: 144 * time.Hour, want: ReplyOutboxManualReview},
	}

	for _, c := range cases {
		entry := newReplyOutboxEntry("message:reclaim-"+c.name, 0, `{"body":"retained"}`)
		_, err := repo.Insert(ctx, entry)
		require.NoError(t, err)

		if c.grants > 0 {
			grantReclaimReplay(ctx, t, store, entry.MessageID)
		}

		now := time.Now()

		_, err = store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version=row_version+1,status='submitting',attempts=?2,operator_replay_grants=?3,
			first_attempt_at=?4,claim_token='reclaim-owner',lease_until=?5 WHERE message_id=?1`,
			entry.MessageID, c.attempts, c.grants, now.Add(-c.age).UnixMilli(), now.Add(-time.Second).UnixMilli())
		require.NoError(t, err)
	}

	result, err := repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{Requeued: 2, SafetyManualReview: 2}, result)

	result, err = repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{}, result)

	for _, c := range cases {
		var (
			status, payload string
			released        bool
		)

		err := store.QueryRow(ctx, `SELECT status,payload,claim_token IS NULL AND lease_until IS NULL FROM bot_reply_outbox WHERE message_id=?1`, "message:reclaim-"+c.name).Scan(&status, &payload, &released)
		require.NoError(t, err)
		require.Equal(t, c.want, status, c.name)
		require.JSONEq(t, `{"body":"retained"}`, payload)
		require.True(t, released)
	}
}

func grantReclaimReplay(ctx context.Context, t *testing.T, store *Store, messageID string) {
	t.Helper()

	id := markReplyManualReview(ctx, t, store, messageID, 0)

	result, err := store.GrantManualReplay(ctx, id, testOperatorEmail, manualReplayReason)
	require.NoError(t, err)
	require.Equal(t, ManualReviewReplayed, result)
}

func TestReplyReclaimRollsBackBatchWhenHorizonPassesBeforeWrite(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	seedReclaimHorizonRows(ctx, t, store, repo)

	horizonMS := repo.automaticReplayHorizon.Milliseconds()
	err := store.write(ctx, func(tx *sql.Tx, now int64) error {
		candidates, err := expiredReplyReclaimCandidates(ctx, tx, now, 10, horizonMS)
		require.NoError(t, err)
		require.Len(t, candidates, 2)

		for _, candidate := range candidates {
			require.False(t, candidate.HorizonExpired)
		}

		_, err = applyReplyReclaim(ctx, tx, candidates, repo.maxAttempts, now+(2100*time.Millisecond).Milliseconds(), horizonMS)

		return err
	})
	require.ErrorIs(t, err, errReplyReclaimSnapshotChanged)

	var unchanged int

	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox WHERE status='submitting' AND claim_token='horizon-owner'`).Scan(&unchanged))
	require.Equal(t, 2, unchanged)

	store.now = func() time.Time { return time.Now().Add(2100 * time.Millisecond) }

	result, err := repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{Requeued: 1, SafetyManualReview: 1}, result)
}

func seedReclaimHorizonRows(ctx context.Context, t *testing.T, store *Store, repo *ReplyOutboxRepository) {
	t.Helper()

	for _, name := range []string{"crossing", "remaining"} {
		entry := newReplyOutboxEntry("message:horizon-"+name, 0, `{"body":"stored"}`)
		_, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
	}

	now := time.Now()
	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version=row_version+1,status='submitting',attempts=1,claim_token='horizon-owner',
		lease_until=?1,
		first_attempt_at=CASE WHEN message_id='message:horizon-crossing' THEN ?2 ELSE ?3 END`,
		now.Add(-time.Second).UnixMilli(), now.Add(-repo.automaticReplayHorizon+2*time.Second).UnixMilli(), now.Add(-time.Hour).UnixMilli())
	require.NoError(t, err)
}
