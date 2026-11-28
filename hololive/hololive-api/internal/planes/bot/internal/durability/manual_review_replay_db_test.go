package durability

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const manualReplayReason = "incident INC-2026-0801 verified Iris repair"

type manualReplayCutoffCase struct {
	name      string
	age       time.Duration
	want      ManualReviewOutcome
	wantClaim bool
}

func TestManualReviewReplayCutoff(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	tests := []manualReplayCutoffCase{
		{name: "before 144 hours", age: 144*time.Hour - time.Minute, want: ManualReviewReplayed, wantClaim: true},
		{name: "at 144 hours", age: 144 * time.Hour, want: ManualReviewCutoffExpired},
		{name: "after 144 hours", age: 144*time.Hour + time.Minute, want: ManualReviewCutoffExpired},
		{name: "at Iris retention", age: 168 * time.Hour, want: ManualReviewCutoffExpired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertManualReplayCutoff(ctx, t, store, tc)
		})
	}
}

func assertManualReplayCutoff(ctx context.Context, t *testing.T, store *Store, tc manualReplayCutoffCase) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay", 0, `{"body":"stored"}`)
	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	var id int64

	err = store.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'manual_review', attempts = 5, claim_token = NULL, lease_until = NULL,
		    last_error = 'manual review required', created_at = ?2
		WHERE message_id = ?1 RETURNING id`, entry.MessageID, time.Now().Add(-tc.age).UnixMilli()).Scan(&id)
	require.NoError(t, err)

	replayOutcome, err := store.GrantManualReplay(ctx, id, testOperatorEmail, manualReplayReason)
	require.NoError(t, err)
	require.Equal(t, tc.want, replayOutcome)

	var auditCount int

	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = ?1", id).Scan(&auditCount))

	if !tc.wantClaim {
		require.Zero(t, auditCount, "rejected replay must not create audit events")
		assertRejectedReplayStaysUnclaimable(ctx, t, store, repo, id)

		return
	}

	require.Equal(t, 1, auditCount)
	assertGrantedReplayIsClaimableAndAudited(ctx, t, store, repo, entry, id)
}

func assertRejectedReplayStaysUnclaimable(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
	id int64,
) {
	t.Helper()

	claim, claimErr := repo.Claim(ctx, "manual-replay-token", time.Minute)
	require.NoError(t, claimErr)
	require.Nil(t, claim, "rejected manual review replay must remain unclaimable")

	var status string

	require.NoError(t, store.QueryRow(ctx, "SELECT status FROM bot_reply_outbox WHERE id = ?1", id).Scan(&status))
	require.Equal(t, ReplyOutboxManualReview, status)
}

func assertGrantedReplayIsClaimableAndAudited(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
	entry *ReplyOutboxEntry,
	id int64,
) {
	t.Helper()

	var (
		actor, reason string
		grantedAt     int64
	)

	require.NoError(t, store.QueryRow(ctx, `SELECT actor, reason, recorded_at
		FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND event_type = 'granted'`, id).Scan(&actor, &reason, &grantedAt))
	require.Equal(t, testOperatorEmail, actor)
	require.Equal(t, manualReplayReason, reason)

	claim, err := repo.Claim(ctx, "manual-replay-token", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, entry.ClientRequestID, claim.ClientRequestID)
	require.JSONEq(t, string(entry.Payload), string(claim.Payload))
	require.Equal(t, int32(6), claim.Attempts, "manual replay preserves attempt history")

	var replayedAt int64

	require.NoError(t, store.QueryRow(ctx, `SELECT recorded_at
		FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND event_type = 'replayed'`, id).Scan(&replayedAt))
	require.GreaterOrEqual(t, replayedAt, grantedAt)

	var replayedActor, replayedReason string

	require.NoError(t, store.QueryRow(ctx, `SELECT actor, reason
		FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND event_type = 'replayed'`, id).Scan(&replayedActor, &replayedReason))
	require.Equal(t, actor, replayedActor, "the replay claim audit carries the granting operator")
	require.Equal(t, reason, replayedReason, "the replay claim audit carries the granted reason")

	var auditCount int

	require.NoError(t, store.QueryRow(ctx,
		`SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = ?1`, id).Scan(&auditCount))
	require.Equal(t, 2, auditCount)

	_, err = store.Exec(ctx, `UPDATE bot_reply_outbox_replay_audit
	SET reason = 'rewritten' WHERE outbox_id = ?1 AND event_type = 'granted'`, id)
	requireConstraintViolation(t, err, "bot_reply_outbox_replay_audit events are immutable")

	_, err = store.Exec(ctx, `DELETE FROM bot_reply_outbox_replay_audit
	WHERE outbox_id = ?1 AND event_type = 'granted'`, id)
	requireConstraintViolation(t, err, "bot_reply_outbox_replay_audit events are immutable")
}

func TestManualReplayClaimRejectsMissingGrantAudit(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay-missing-grant", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	var id int64

	require.NoError(t, store.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'pending', attempts = 5, operator_replay_grants = 1
		WHERE message_id = ?1 RETURNING id`, entry.MessageID).Scan(&id))

	claim, err := repo.Claim(ctx, "missing-grant-claim", time.Minute)
	requireConstraintViolation(t, err, "manual replay grant audit is missing")
	require.Nil(t, claim)

	var (
		status   string
		attempts int32
	)

	require.NoError(t, store.QueryRow(ctx,
		`SELECT status, attempts FROM bot_reply_outbox WHERE id = ?1`, id).Scan(&status, &attempts))
	require.Equal(t, "pending", status)
	require.Equal(t, int32(5), attempts, "rejected claim transaction must not advance attempts")

	var replayedCount int

	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND event_type = 'replayed'`, id).Scan(&replayedCount))
	require.Zero(t, replayedCount)
}

func TestManualReviewReplayRejectsUnboundedOrUnsafeMetadata(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay-metadata", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	id := markReplyManualReview(ctx, t, store, entry.MessageID, 0)

	for _, input := range []struct{ actor, reason string }{
		{actor: "operator with spaces", reason: "ticket-1"},
		{actor: testOperatorEmail, reason: strings.Repeat("r", 257)},
		{actor: testOperatorEmail, reason: "bad\nreason"},
		{actor: testOperatorEmail, reason: "   "},
	} {
		outcome, err := store.GrantManualReplay(ctx, id, input.actor, input.reason)
		require.NoError(t, err)
		require.Equal(t, ManualReviewInvalidOperatorMetadata, outcome)
	}

	var (
		status     string
		auditCount int
	)

	require.NoError(t, store.QueryRow(ctx,
		"SELECT status FROM bot_reply_outbox WHERE id = ?1", id).Scan(&status))
	require.Equal(t, ReplyOutboxManualReview, status)
	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = ?1", id).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewReplayReportsMissingAndActiveRows(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay-active", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	outcome, err := store.GrantManualReplay(ctx, 999, testOperatorEmail, manualReplayReason)
	require.NoError(t, err)
	require.Equal(t, ManualReviewNotFound, outcome)

	var id int64

	require.NoError(t, store.QueryRow(ctx, "SELECT id FROM bot_reply_outbox WHERE message_id = ?1", entry.MessageID).Scan(&id))

	outcome, err = store.GrantManualReplay(ctx, id, testOperatorEmail, manualReplayReason)
	require.NoError(t, err)
	require.Equal(t, ManualReviewNotManualReview, outcome)
}

func TestManualReviewReplayGrantsExactlyOnceUnderConcurrency(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay-concurrent", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	id := markReplyManualReview(ctx, t, store, entry.MessageID, 0)

	counts := runConcurrentManualReplays(ctx, t, store, id)
	require.Equal(t, 1, counts[ManualReviewReplayed])
	require.Equal(t, 1, counts[ManualReviewNotManualReview])

	var grants, auditCount int

	require.NoError(t, store.QueryRow(ctx,
		"SELECT operator_replay_grants FROM bot_reply_outbox WHERE id = ?1", id).Scan(&grants))
	require.Equal(t, 1, grants)
	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id = ?1", id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)

	require.Equal(t, 1, countConcurrentOutboxClaims(ctx, t, repo))
	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND event_type = 'replayed'`, id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func runConcurrentManualReplays(
	ctx context.Context,
	t *testing.T,
	store *Store,
	id int64,
) map[ManualReviewOutcome]int {
	t.Helper()

	start := make(chan struct{})
	outcomes := make(chan ManualReviewOutcome, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup

	for i := range 2 {
		wg.Go(func() {
			<-start

			outcome, replayErr := store.GrantManualReplay(ctx, id, testOperatorEmail, fmt.Sprintf("concurrent grant attempt %d", i))
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

	counts := map[ManualReviewOutcome]int{}

	for outcome := range outcomes {
		counts[outcome]++
	}

	return counts
}

func countConcurrentOutboxClaims(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository) int {
	t.Helper()

	start := make(chan struct{})
	claims := make(chan *ReplyOutboxClaim, 2)
	claimErrs := make(chan error, 2)

	var wg sync.WaitGroup

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

	return claimCount
}

func TestManualReviewReplayGrantAllowsRetryableReclaimWithoutDuplicateAudit(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-replay-retryable", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	id := markReplyManualReview(ctx, t, store, entry.MessageID, 1)

	outcome, err := store.GrantManualReplay(ctx, id, testOperatorEmail, "incident INC-2026-0801 authorized retryable replay")
	require.NoError(t, err)
	require.Equal(t, ManualReviewReplayed, outcome)

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
	require.NoError(t, makeReplyRetryDue(ctx, store, first.ID, time.Millisecond))

	second, err := repo.Claim(ctx, "manual-replay-retryable-b", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, int32(3), second.Attempts)
	require.Equal(t, entry.ClientRequestID, second.ClientRequestID)

	var grantedCount, replayedCount int

	require.NoError(t, store.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE event_type = 'granted'),
		count(*) FILTER (WHERE event_type = 'replayed')
		FROM bot_reply_outbox_replay_audit
		WHERE outbox_id = ?1 AND grant_number = 1`, id).Scan(&grantedCount, &replayedCount))
	require.Equal(t, 1, grantedCount)
	require.Equal(t, 1, replayedCount)
}

func markReplyManualReview(ctx context.Context, t *testing.T, store *Store, messageID string, attempts int32) int64 {
	t.Helper()

	var id int64

	require.NoError(t, store.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'manual_review', attempts = ?2
		WHERE message_id = ?1 RETURNING id`, messageID, attempts).Scan(&id))

	return id
}
