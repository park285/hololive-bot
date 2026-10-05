package durability

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestReplyReclaimPreservesAcceptedSafetyGrantsAndLockedRows(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := t.Context()
	cases := []struct {
		name, status, want string
		attempts, grants   int32
		age                int64
	}{
		{name: "accepted", status: replyOutboxStatusAccepted, attempts: 1, age: 1, want: ReplyOutboxManualReview},
		{name: "retry", status: replyOutboxStatusSubmitting, attempts: 4, age: 1, want: replyOutboxStatusPending},
		{name: "exhausted", status: replyOutboxStatusSubmitting, attempts: 5, age: 1, want: ReplyOutboxManualReview},
		{name: "grant", status: replyOutboxStatusSubmitting, attempts: 5, grants: 1, age: 1, want: replyOutboxStatusPending},
		{name: "horizon", status: replyOutboxStatusSubmitting, attempts: 1, age: 144, want: ReplyOutboxManualReview},
	}

	for _, c := range cases {
		entry := newReplyOutboxEntry("message:reclaim-"+c.name, 0, `{"body":"retained"}`)
		_, err := repo.Insert(ctx, entry)
		require.NoError(t, err)

		if c.grants > 0 {
			grantReclaimReplay(ctx, t, pool, repo, entry.MessageID)
		}

		_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox SET status=$2,attempts=$3,operator_replay_grants=$4,
			first_attempt_at=clock_timestamp()-($5::bigint*interval '1 hour'),
			iris_request_id=CASE WHEN $2='accepted' THEN 'iris-accepted' ELSE '' END,
			claim_token='reclaim-owner',lease_until=clock_timestamp()-interval '1 second' WHERE message_id=$1`,
			entry.MessageID, c.status, c.attempts, c.grants, c.age)
		require.NoError(t, err)
	}

	locker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, rollbackInboxTx(ctx, locker)) }()

	_, err = locker.Exec(ctx, `SELECT id FROM bot_reply_outbox WHERE message_id='message:reclaim-accepted' FOR UPDATE`)
	require.NoError(t, err)

	result, err := repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{Requeued: 2, SafetyManualReview: 2}, result)
	require.NoError(t, locker.Commit(ctx))

	result, err = repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{AcceptedManualReview: 1}, result)

	for _, c := range cases {
		var (
			status, payload string
			released        bool
		)

		err := pool.QueryRow(ctx, `SELECT status,payload::text,claim_token IS NULL AND lease_until IS NULL FROM bot_reply_outbox WHERE message_id=$1`, "message:reclaim-"+c.name).Scan(&status, &payload, &released)
		require.NoError(t, err)
		require.Equal(t, c.want, status, c.name)
		require.JSONEq(t, `{"body":"retained"}`, payload)
		require.True(t, released)
	}
}

func grantReclaimReplay(ctx context.Context, t *testing.T, pool *pgxpool.Pool, repo *ReplyOutboxRepository, messageID string) {
	t.Helper()

	var id int64

	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox SET status='manual_review' WHERE message_id=$1 RETURNING id`, messageID).Scan(&id))

	result, err := repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{OutboxID: id, Actor: testOperatorEmail, Reason: manualReplayReason})
	require.NoError(t, err)
	require.Equal(t, "replayed", result)
}

func TestReplyReclaimRollsBackBatchWhenHorizonPassesBeforeWrite(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	seedReclaimHorizonRows(ctx, t, pool, repo)

	horizonMS := ReplyOutboxAutomaticReplayHorizon.Milliseconds()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		candidates, err := lockedReplyReclaimCandidates(ctx, tx, 10, horizonMS)
		require.NoError(t, err)
		require.Len(t, candidates, 2)

		for _, candidate := range candidates {
			require.False(t, candidate.HorizonExpired)
		}

		_, err = pool.Exec(ctx, `SELECT pg_sleep(2.1)`)
		require.NoError(t, err)

		_, err = applyReplyReclaim(ctx, tx, candidates, ReplyOutboxMaxAttempts, horizonMS)

		return err
	})
	require.ErrorContains(t, err, "snapshot no longer eligible")

	var unchanged int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox WHERE status='submitting' AND claim_token='horizon-owner'`).Scan(&unchanged))
	require.Equal(t, 2, unchanged)

	result, err := repo.ReclaimExpired(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxReclaim{Requeued: 1, SafetyManualReview: 1}, result)
}

func seedReclaimHorizonRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, repo *ReplyOutboxRepository) {
	t.Helper()

	for _, name := range []string{"crossing", "remaining"} {
		entry := newReplyOutboxEntry("message:horizon-"+name, 0, `{"body":"stored"}`)
		_, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, `UPDATE bot_reply_outbox SET status='submitting',attempts=1,claim_token='horizon-owner',
		lease_until=clock_timestamp()-interval '1 second',
		first_attempt_at=CASE WHEN message_id='message:horizon-crossing' THEN clock_timestamp()-interval '144 hours'+interval '2 seconds' ELSE clock_timestamp()-interval '1 hour' END`)
	require.NoError(t, err)
}
