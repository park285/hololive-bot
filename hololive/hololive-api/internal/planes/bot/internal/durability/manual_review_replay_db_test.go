package durability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
)

func TestManualReviewReplayCutoff(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		createdAt string
		want      string
		wantClaim bool
	}{
		{name: "before 144 hours", createdAt: "clock_timestamp() - interval '144 hours' + interval '1 minute'", want: "replayed", wantClaim: true},
		{name: "at 144 hours", createdAt: "clock_timestamp() - interval '144 hours'", want: "cutoff_expired"},
		{name: "after 144 hours", createdAt: "clock_timestamp() - interval '144 hours' - interval '1 minute'", want: "cutoff_expired"},
		{name: "at Iris retention", createdAt: "clock_timestamp() - interval '168 hours'", want: "cutoff_expired"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			truncateDurabilityTables(ctx, t, pool)
			repo := NewReplyOutboxRepository(pool)
			entry := newReplyOutboxEntry("message:manual-replay", 0, `{"body":"stored"}`)
			outcome, err := repo.Insert(ctx, entry)
			require.NoError(t, err)
			require.Equal(t, ReplyOutboxInserted, outcome)

			var id int64
			err = pool.QueryRow(ctx, `UPDATE bot_reply_outbox
				SET status = 'manual_review', attempts = 5, claim_token = NULL, lease_until = NULL,
				    last_error = 'manual review required', created_at = `+tc.createdAt+`
				WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id)
			require.NoError(t, err)

			replayOutcome, err := repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
				OutboxID: id,
				Actor:    "operator@example.com",
				Reason:   "incident INC-2026-0801 verified Iris repair",
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, replayOutcome)
			var auditCount int
			require.NoError(t, pool.QueryRow(ctx,
				"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1", id).Scan(&auditCount))

			if !tc.wantClaim {
				require.Zero(t, auditCount, "rejected replay must not create audit events")
				claim, claimErr := repo.Claim(ctx, "manual-replay-token", time.Minute)
				require.NoError(t, claimErr)
				require.Nil(t, claim, "rejected manual review replay must remain unclaimable")
				var status string
				require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM bot_reply_outbox WHERE id = $1", id).Scan(&status))
				require.Equal(t, ReplyOutboxManualReview, status)
				return
			}

			require.Equal(t, 1, auditCount)
			var actor, reason string
			var grantedAt time.Time
			require.NoError(t, pool.QueryRow(ctx, `SELECT actor, reason, recorded_at
				FROM bot_reply_outbox_replay_audit
				WHERE outbox_id = $1 AND event_type = 'granted'`, id).Scan(&actor, &reason, &grantedAt))
			require.Equal(t, "operator@example.com", actor)
			require.Equal(t, "incident INC-2026-0801 verified Iris repair", reason)

			claim, err := repo.Claim(ctx, "manual-replay-token", time.Minute)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, entry.ClientRequestID, claim.ClientRequestID)
			require.JSONEq(t, string(entry.Payload), string(claim.Payload))
			require.Equal(t, int32(6), claim.Attempts, "manual replay preserves attempt history")
			var replayedAt time.Time
			require.NoError(t, pool.QueryRow(ctx, `SELECT recorded_at
				FROM bot_reply_outbox_replay_audit
				WHERE outbox_id = $1 AND event_type = 'replayed'`, id).Scan(&replayedAt))
			require.False(t, replayedAt.Before(grantedAt))
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1`, id).Scan(&auditCount))
			require.Equal(t, 2, auditCount)
			_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox_replay_audit
			SET reason = 'rewritten' WHERE outbox_id = $1 AND event_type = 'granted'`, id)
			requirePGErrorCode(t, err, "55000")
			_, err = pool.Exec(ctx, `DELETE FROM bot_reply_outbox_replay_audit
			WHERE outbox_id = $1 AND event_type = 'granted'`, id)
			requirePGErrorCode(t, err, "55000")
		})
	}
}

func TestManualReplayAuditRuntimePrivileges(t *testing.T) {
	ownerPool, runtimePool, runtimeRole := newAuditPrivilegePools(t)
	ctx := context.Background()
	ownerRepo := NewReplyOutboxRepository(ownerPool)
	runtimeRepo := NewReplyOutboxRepository(runtimePool)
	entry := newReplyOutboxEntry("message:manual-replay-runtime-privileges", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, ownerRepo, entry))

	var id int64
	require.NoError(t, ownerPool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review', attempts = 5
		WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))
	outcome, err := ownerRepo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
		OutboxID: id,
		Actor:    "operator@example.com",
		Reason:   "incident INC-2026-0801 authorized replay",
	})
	require.NoError(t, err)
	require.Equal(t, "replayed", outcome)

	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		var allowed bool
		require.NoError(t, ownerPool.QueryRow(ctx,
			`SELECT has_table_privilege($1, 'public.bot_reply_outbox_replay_audit', $2)`,
			runtimeRole, privilege).Scan(&allowed))
		require.False(t, allowed, "runtime role retained %s on replay audit", privilege)
	}
	var sequenceAllowed, grantFunctionAllowed bool
	require.NoError(t, ownerPool.QueryRow(ctx,
		`SELECT has_sequence_privilege($1, 'public.bot_reply_outbox_replay_audit_id_seq', 'USAGE')`,
		runtimeRole).Scan(&sequenceAllowed))
	require.False(t, sequenceAllowed)
	require.NoError(t, ownerPool.QueryRow(ctx,
		`SELECT has_function_privilege($1, 'public.grant_bot_reply_outbox_manual_replay(bigint,text,text)', 'EXECUTE')`,
		runtimeRole).Scan(&grantFunctionAllowed))
	require.False(t, grantFunctionAllowed)

	_, err = runtimePool.Exec(ctx, `INSERT INTO bot_reply_outbox_replay_audit
		(outbox_id, grant_number, event_type, actor, reason)
		VALUES ($1, 2, 'granted', 'forged', 'forged grant')`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx, `UPDATE bot_reply_outbox_replay_audit
		SET reason = 'forged update' WHERE outbox_id = $1`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx, `DELETE FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx,
		`SELECT public.grant_bot_reply_outbox_manual_replay($1, 'forged', 'forged grant')`, id)
	requireInsufficientPrivilege(t, err)

	claim, err := runtimeRepo.Claim(ctx, "runtime-authorized-claim", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)
	var grantedActor, grantedReason, replayedActor, replayedReason string
	require.NoError(t, ownerPool.QueryRow(ctx, `SELECT
		max(actor) FILTER (WHERE event_type = 'granted'),
		max(reason) FILTER (WHERE event_type = 'granted'),
		max(actor) FILTER (WHERE event_type = 'replayed'),
		max(reason) FILTER (WHERE event_type = 'replayed')
		FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1`, id).Scan(
		&grantedActor, &grantedReason, &replayedActor, &replayedReason))
	require.Equal(t, grantedActor, replayedActor)
	require.Equal(t, grantedReason, replayedReason)
}

func TestManualReplayClaimRejectsMissingGrantAudit(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-replay-missing-grant", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))
	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'pending', attempts = 5, operator_replay_grants = 1
		WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))

	claim, err := repo.Claim(ctx, "missing-grant-claim", time.Minute)
	requirePGErrorCode(t, err, "23514")
	require.Nil(t, claim)
	var status string
	var attempts int32
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, attempts FROM bot_reply_outbox WHERE id = $1`, id).Scan(&status, &attempts))
	require.Equal(t, "pending", status)
	require.Equal(t, int32(5), attempts, "rejected claim transaction must not advance attempts")
	var replayedCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = $1 AND event_type = 'replayed'`, id).Scan(&replayedCount))
	require.Zero(t, replayedCount)
}

func TestManualReviewReplayRejectsUnboundedOrUnsafeMetadata(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-replay-metadata", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))
	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review' WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))

	_, err := repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
		OutboxID: id, Actor: "operator with spaces", Reason: "ticket-1",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
		OutboxID: id, Actor: "operator@example.com", Reason: strings.Repeat("r", 257),
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	var outcome string
	err = pool.QueryRow(ctx, replyOutboxReplayManualReviewSQL, id, "operator@example.com", "bad\nreason").Scan(&outcome)
	require.NoError(t, err)
	require.Equal(t, "invalid_operator_metadata", outcome)
	var status string
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status FROM bot_reply_outbox WHERE id = $1", id).Scan(&status))
	require.Equal(t, ReplyOutboxManualReview, status)
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1", id).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewReplayGrantsExactlyOnceUnderConcurrency(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-replay-concurrent", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))
	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review' WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))

	start := make(chan struct{})
	outcomes := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			<-start
			outcome, replayErr := repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
				OutboxID: id,
				Actor:    "operator@example.com",
				Reason:   fmt.Sprintf("concurrent grant attempt %d", i),
			})
			outcomes <- outcome
			errs <- replayErr
		})
	}
	close(start)
	wg.Wait()
	close(outcomes)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	counts := map[string]int{}
	for outcome := range outcomes {
		counts[outcome]++
	}
	require.Equal(t, 1, counts["replayed"])
	require.Equal(t, 1, counts["not_manual_review"])
	var grants, auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT operator_replay_grants FROM bot_reply_outbox WHERE id = $1", id).Scan(&grants))
	require.Equal(t, 1, grants)
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = $1", id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)

	claims := make(chan *ReplyOutboxClaim, 2)
	claimErrs := make(chan error, 2)
	start = make(chan struct{})
	for i := range 2 {
		wg.Go(func() {
			<-start
			claim, claimErr := repo.Claim(ctx, fmt.Sprintf("concurrent-claim-%d", i), time.Minute)
			claims <- claim
			claimErrs <- claimErr
		})
	}
	close(start)
	wg.Wait()
	close(claims)
	close(claimErrs)
	for claimErr := range claimErrs {
		require.NoError(t, claimErr)
	}
	claimCount := 0
	for claim := range claims {
		if claim != nil {
			claimCount++
		}
	}
	require.Equal(t, 1, claimCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = $1 AND event_type = 'replayed'`, id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestManualReviewReplayGrantAllowsRetryableReclaimWithoutDuplicateAudit(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-replay-retryable", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))

	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review', attempts = 1
		WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))
	outcome, err := repo.ReplayManualReview(ctx, ReplyOutboxManualReplay{
		OutboxID: id,
		Actor:    "operator@example.com",
		Reason:   "incident INC-2026-0801 authorized retryable replay",
	})
	require.NoError(t, err)
	require.Equal(t, "replayed", outcome)

	first, err := repo.Claim(ctx, "manual-replay-retryable-a", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, int32(2), first.Attempts)
	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID:         first.ID,
		ClaimToken: "manual-replay-retryable-a",
		Status:     ReplyOutboxRetryablePreDispatch,
		LastError:  "pre-dispatch transport reset",
		RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)
	time.Sleep(5 * time.Millisecond)

	second, err := repo.Claim(ctx, "manual-replay-retryable-b", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, int32(3), second.Attempts)
	require.Equal(t, entry.ClientRequestID, second.ClientRequestID)

	var grantedCount, replayedCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE event_type = 'granted'),
		count(*) FILTER (WHERE event_type = 'replayed')
		FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = $1 AND grant_number = 1`, id).Scan(&grantedCount, &replayedCount))
	require.Equal(t, 1, grantedCount)
	require.Equal(t, 1, replayedCount)
}

func newAuditPrivilegePools(t *testing.T) (
	ownerPool *pgxpool.Pool,
	runtimePool *pgxpool.Pool,
	runtimeRole string,
) {
	t.Helper()
	ctx := context.Background()
	ownerPool = dbtest.NewBlankPool(t)
	runtimeRole = fmt.Sprintf("hololive_runtime_audit_%d", time.Now().UnixNano())
	quotedRole := pgx.Identifier{runtimeRole}.Sanitize()
	_, err := ownerPool.Exec(ctx, "CREATE ROLE "+quotedRole+" NOLOGIN")
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, statement := range []string{
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM " + quotedRole,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM " + quotedRole,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM " + quotedRole,
			"DROP OWNED BY " + quotedRole,
			"DROP ROLE " + quotedRole,
		} {
			if _, cleanupErr := ownerPool.Exec(context.Background(), statement); cleanupErr != nil {
				t.Errorf("cleanup audit runtime role: %v", cleanupErr)
			}
		}
	})

	for _, statement := range []string{
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + quotedRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + quotedRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + quotedRole,
	} {
		_, err = ownerPool.Exec(ctx, statement)
		require.NoError(t, err)
	}
	require.NoError(t, dbtest.ApplyMigrations(ctx, ownerPool))
	_, err = ownerPool.Exec(ctx, "GRANT USAGE ON SCHEMA public TO "+quotedRole)
	require.NoError(t, err)

	runtimeConfig := ownerPool.Config()
	runtimeConfig.ConnConfig.RuntimeParams["role"] = runtimeRole
	runtimePool, err = pgxpool.NewWithConfig(ctx, runtimeConfig)
	require.NoError(t, err)
	t.Cleanup(runtimePool.Close)

	return ownerPool, runtimePool, runtimeRole
}

func requireInsufficientPrivilege(t *testing.T, err error) {
	t.Helper()
	requirePGErrorCode(t, err, "42501")
}

func requirePGErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, code, pgErr.Code)
}
