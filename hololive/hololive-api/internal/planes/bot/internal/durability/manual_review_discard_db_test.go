// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package durability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManualReviewDiscardWithoutReplay(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-discard", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))

	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review', attempts = 1
		WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))
	_, err := pool.Exec(ctx, `UPDATE bot_reply_outbox
		SET status = 'discarded', payload = NULL WHERE id = $1`, id)
	requirePGErrorCode(t, err, "23514")

	var outcome string
	require.NoError(t, pool.QueryRow(ctx, `SELECT public.discard_bot_reply_outbox_manual_review(
		$1, $2, $3, $4)`, id, "operator@example.com",
		"incident INC-2026-0818 Iris outcome remains unknown", "outcome_unknown").Scan(&outcome))
	require.Equal(t, "discarded", outcome)

	var status, lastError string
	var payloadIsNull, claimTokenIsNull, leaseUntilIsNull bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT status, payload IS NULL, claim_token IS NULL,
		lease_until IS NULL, last_error FROM bot_reply_outbox WHERE id = $1`, id).Scan(
		&status, &payloadIsNull, &claimTokenIsNull, &leaseUntilIsNull, &lastError))
	require.Equal(t, ReplyOutboxDiscarded, status)
	require.True(t, payloadIsNull)
	require.True(t, claimTokenIsNull)
	require.True(t, leaseUntilIsNull)
	require.Equal(t, "operator discarded manual review without replay", lastError)

	var decision, irisState, actor, reason string
	require.NoError(t, pool.QueryRow(ctx, `SELECT decision, observed_iris_state, actor, reason
		FROM bot_reply_outbox_resolution_audit WHERE outbox_id = $1`, id).Scan(
		&decision, &irisState, &actor, &reason))
	require.Equal(t, "discarded_without_replay", decision)
	require.Equal(t, "outcome_unknown", irisState)
	require.Equal(t, "operator@example.com", actor)
	require.Equal(t, "incident INC-2026-0818 Iris outcome remains unknown", reason)

	stats, err := repo.ManualReviewStats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Backlog)
	claim, err := repo.Claim(ctx, "manual-discard-claim", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claim)

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox_resolution_audit
		SET reason = 'rewritten' WHERE outbox_id = $1`, id)
	requirePGErrorCode(t, err, "55000")
	_, err = pool.Exec(ctx, `DELETE FROM bot_reply_outbox_resolution_audit WHERE outbox_id = $1`, id)
	requirePGErrorCode(t, err, "55000")

	_, err = pool.Exec(ctx, `UPDATE bot_reply_outbox
		SET updated_at = clock_timestamp() - interval '9 days' WHERE id = $1`, id)
	require.NoError(t, err)
	result, err := NewDurableLedgerRepository(pool).Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.DeletedOutbox)
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit WHERE outbox_id = $1", id).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewDiscardRejectsInvalidInput(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	manual := newReplyOutboxEntry("message:manual-discard-invalid", 0, `{"body":"stored"}`)
	pending := newReplyOutboxEntry("message:manual-discard-pending", 0, `{"body":"pending"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, manual))
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, pending))

	var manualID, pendingID int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review' WHERE message_id = $1 RETURNING id`, manual.MessageID).Scan(&manualID))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT id FROM bot_reply_outbox WHERE message_id = $1", pending.MessageID).Scan(&pendingID))

	tests := []struct {
		name    string
		id      int64
		actor   any
		reason  any
		state   any
		outcome string
	}{
		{name: "missing row", id: -1, actor: "operator", reason: "incident", state: "outcome_unknown", outcome: "not_found"},
		{name: "not manual review", id: pendingID, actor: "operator", reason: "incident", state: "outcome_unknown", outcome: "not_manual_review"},
		{name: "invalid actor", id: manualID, actor: "operator with spaces", reason: "incident", state: "outcome_unknown", outcome: "invalid_operator_metadata"},
		{name: "missing actor", id: manualID, actor: nil, reason: "incident", state: "outcome_unknown", outcome: "invalid_operator_metadata"},
		{name: "control character in reason", id: manualID, actor: "operator", reason: "incident\nunsafe", state: "outcome_unknown", outcome: "invalid_operator_metadata"},
		{name: "unknown Iris state", id: manualID, actor: "operator", reason: "incident", state: "maybe_sent", outcome: "invalid_iris_state"},
		{name: "missing Iris state", id: manualID, actor: "operator", reason: "incident", state: nil, outcome: "invalid_iris_state"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outcome string
			require.NoError(t, pool.QueryRow(ctx, `SELECT public.discard_bot_reply_outbox_manual_review(
				$1, $2, $3, $4)`, tc.id, tc.actor, tc.reason, tc.state).Scan(&outcome))
			require.Equal(t, tc.outcome, outcome)
		})
	}

	var status string
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT status FROM bot_reply_outbox WHERE id = $1", manualID).Scan(&status))
	require.Equal(t, ReplyOutboxManualReview, status)
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit").Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewDiscardIsExactlyOnceUnderConcurrency(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)
	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:manual-discard-concurrent", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, repo, entry))

	var id int64
	require.NoError(t, pool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review' WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))

	start := make(chan struct{})
	outcomes := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			var outcome string
			err := pool.QueryRow(ctx, `SELECT public.discard_bot_reply_outbox_manual_review(
				$1, $2, $3, $4)`, id, "operator@example.com",
				"incident INC-2026-0818 concurrent decision", "outcome_unknown").Scan(&outcome)
			outcomes <- outcome
			errs <- err
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
	require.Equal(t, 1, counts["discarded"])
	require.Equal(t, 1, counts["not_manual_review"])

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit WHERE outbox_id = $1", id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestManualReviewResolutionAuditRuntimePrivileges(t *testing.T) {
	ownerPool, runtimePool, runtimeRole := newAuditPrivilegePools(t)
	ctx := context.Background()
	ownerRepo := NewReplyOutboxRepository(ownerPool)
	entry := newReplyOutboxEntry("message:manual-discard-runtime-privileges", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t, ctx, ownerRepo, entry))

	var id int64
	require.NoError(t, ownerPool.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status = 'manual_review' WHERE message_id = $1 RETURNING id`, entry.MessageID).Scan(&id))
	var outcome string
	require.NoError(t, ownerPool.QueryRow(ctx, `SELECT public.discard_bot_reply_outbox_manual_review(
		$1, $2, $3, $4)`, id, "operator@example.com",
		"incident INC-2026-0818 authorized discard", "outcome_unknown").Scan(&outcome))
	require.Equal(t, "discarded", outcome)

	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		var allowed bool
		require.NoError(t, ownerPool.QueryRow(ctx,
			`SELECT has_table_privilege($1, 'public.bot_reply_outbox_resolution_audit', $2)`,
			runtimeRole, privilege).Scan(&allowed))
		require.False(t, allowed, "runtime role retained %s on resolution audit", privilege)
	}
	var sequenceAllowed, functionAllowed bool
	require.NoError(t, ownerPool.QueryRow(ctx,
		`SELECT has_sequence_privilege($1, 'public.bot_reply_outbox_resolution_audit_id_seq', 'USAGE')`,
		runtimeRole).Scan(&sequenceAllowed))
	require.False(t, sequenceAllowed)
	require.NoError(t, ownerPool.QueryRow(ctx,
		`SELECT has_function_privilege($1, 'public.discard_bot_reply_outbox_manual_review(bigint,text,text,text)', 'EXECUTE')`,
		runtimeRole).Scan(&functionAllowed))
	require.False(t, functionAllowed)
	var triggerFunctionAllowed bool
	require.NoError(t, ownerPool.QueryRow(ctx,
		`SELECT has_function_privilege($1, 'public.enforce_bot_reply_outbox_discard_audit()', 'EXECUTE')`,
		runtimeRole).Scan(&triggerFunctionAllowed))
	require.False(t, triggerFunctionAllowed)

	_, err := runtimePool.Exec(ctx, `INSERT INTO bot_reply_outbox_resolution_audit
		(outbox_id, decision, observed_iris_state, actor, reason)
		VALUES ($1, 'discarded_without_replay', 'outcome_unknown', 'forged', 'forged decision')`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx, `UPDATE bot_reply_outbox_resolution_audit
		SET reason = 'forged update' WHERE outbox_id = $1`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx, `DELETE FROM bot_reply_outbox_resolution_audit WHERE outbox_id = $1`, id)
	requireInsufficientPrivilege(t, err)
	_, err = runtimePool.Exec(ctx, `SELECT public.discard_bot_reply_outbox_manual_review(
		$1, 'forged', 'forged decision', 'outcome_unknown')`, id)
	requireInsufficientPrivilege(t, err)
}
