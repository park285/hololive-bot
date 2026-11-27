package durability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testDiscardActor            = "operator"
	testDiscardReason           = "incident"
	testIrisStateOutcomeUnknown = "outcome_unknown"
	testDiscardIncidentReason   = "incident INC-2026-0818 Iris outcome remains unknown"
)

func TestManualReviewDiscardWithoutReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-discard", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	id := discardManualReviewWithoutReplay(ctx, t, store, entry)

	assertDiscardedRowIsScrubbed(ctx, t, store, id)
	assertDiscardAuditRecordsTheDecision(ctx, t, store, id)

	stats, err := repo.ManualReviewStats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Backlog)

	claim, err := repo.Claim(ctx, "manual-discard-claim", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claim)

	assertDiscardAuditIsAppendOnly(ctx, t, store, id)
	assertDiscardAuditIsPrunedWithTheRow(ctx, t, store, id)
}

func discardManualReviewWithoutReplay(
	ctx context.Context,
	t *testing.T,
	store *Store,
	entry *ReplyOutboxEntry,
) int64 {
	t.Helper()

	id := markReplyManualReview(ctx, t, store, entry.MessageID, 1)

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'discarded', payload = NULL WHERE id = ?1`, id)
	requireConstraintViolation(t, err, "discarded reply transition requires an audited manual-review decision")

	outcome, err := store.DiscardManualReview(ctx, id, testOperatorEmail, testDiscardIncidentReason, testIrisStateOutcomeUnknown)
	require.NoError(t, err)
	require.Equal(t, ManualReviewDiscarded, outcome)

	return id
}

func assertDiscardedRowIsScrubbed(ctx context.Context, t *testing.T, store *Store, id int64) {
	t.Helper()

	var (
		status, lastError                                 string
		payloadIsNull, claimTokenIsNull, leaseUntilIsNull bool
	)

	require.NoError(t, store.QueryRow(ctx, `SELECT status, payload IS NULL, claim_token IS NULL,
		lease_until IS NULL, last_error FROM bot_reply_outbox WHERE id = ?1`, id).Scan(
		&status, &payloadIsNull, &claimTokenIsNull, &leaseUntilIsNull, &lastError))
	require.Equal(t, ReplyOutboxDiscarded, status)
	require.True(t, payloadIsNull)
	require.True(t, claimTokenIsNull)
	require.True(t, leaseUntilIsNull)
	require.Equal(t, "operator discarded manual review without replay", lastError)
}

func assertDiscardAuditRecordsTheDecision(ctx context.Context, t *testing.T, store *Store, id int64) {
	t.Helper()

	var decision, irisState, actor, reason string

	require.NoError(t, store.QueryRow(ctx, `SELECT decision, observed_iris_state, actor, reason
		FROM bot_reply_outbox_resolution_audit WHERE outbox_id = ?1`, id).Scan(
		&decision, &irisState, &actor, &reason))
	require.Equal(t, "discarded_without_replay", decision)
	require.Equal(t, testIrisStateOutcomeUnknown, irisState)
	require.Equal(t, testOperatorEmail, actor)
	require.Equal(t, testDiscardIncidentReason, reason)
}

func assertDiscardAuditIsAppendOnly(ctx context.Context, t *testing.T, store *Store, id int64) {
	t.Helper()

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox_resolution_audit
		SET reason = 'rewritten' WHERE outbox_id = ?1`, id)
	requireConstraintViolation(t, err, "bot_reply_outbox_resolution_audit events are immutable")

	_, err = store.Exec(ctx, `DELETE FROM bot_reply_outbox_resolution_audit WHERE outbox_id = ?1`, id)
	requireConstraintViolation(t, err, "bot_reply_outbox_resolution_audit events are immutable")
}

func assertDiscardAuditIsPrunedWithTheRow(ctx context.Context, t *testing.T, store *Store, id int64) {
	t.Helper()

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, updated_at = ?2 WHERE id = ?1`, id, time.Now().Add(-9*24*time.Hour).UnixMilli())
	require.NoError(t, err)

	result, err := NewDurableLedgerRepository(store).Maintain(ctx, 8*24*time.Hour, 30*24*time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.DeletedOutbox)

	var auditCount int

	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit WHERE outbox_id = ?1", id).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewDiscardRejectsInvalidInput(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	manual := newReplyOutboxEntry("message:manual-discard-invalid", 0, `{"body":"stored"}`)
	pending := newReplyOutboxEntry("message:manual-discard-pending", 0, `{"body":"pending"}`)

	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, manual))
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, pending))

	manualID := markReplyManualReview(ctx, t, store, manual.MessageID, 0)

	var pendingID int64

	require.NoError(t, store.QueryRow(ctx,
		"SELECT id FROM bot_reply_outbox WHERE message_id = ?1", pending.MessageID).Scan(&pendingID))

	tests := []struct {
		name    string
		id      int64
		actor   string
		reason  string
		state   string
		outcome ManualReviewOutcome
	}{
		{name: "missing row", id: -1, actor: testDiscardActor, reason: testDiscardReason, state: testIrisStateOutcomeUnknown, outcome: ManualReviewNotFound},
		{name: "not manual review", id: pendingID, actor: testDiscardActor, reason: testDiscardReason, state: testIrisStateOutcomeUnknown, outcome: ManualReviewNotManualReview},
		{name: "invalid actor", id: manualID, actor: "operator with spaces", reason: testDiscardReason, state: testIrisStateOutcomeUnknown, outcome: ManualReviewInvalidOperatorMetadata},
		{name: "missing actor", id: manualID, actor: "", reason: testDiscardReason, state: testIrisStateOutcomeUnknown, outcome: ManualReviewInvalidOperatorMetadata},
		{name: "control character in reason", id: manualID, actor: testDiscardActor, reason: "incident\nunsafe", state: testIrisStateOutcomeUnknown, outcome: ManualReviewInvalidOperatorMetadata},
		{name: "unknown Iris state", id: manualID, actor: testDiscardActor, reason: testDiscardReason, state: "maybe_sent", outcome: ManualReviewInvalidIrisState},
		{name: "missing Iris state", id: manualID, actor: testDiscardActor, reason: testDiscardReason, state: "", outcome: ManualReviewInvalidIrisState},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := store.DiscardManualReview(ctx, tc.id, tc.actor, tc.reason, tc.state)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, outcome)
		})
	}

	var (
		status     string
		auditCount int
	)

	require.NoError(t, store.QueryRow(ctx,
		"SELECT status FROM bot_reply_outbox WHERE id = ?1", manualID).Scan(&status))
	require.Equal(t, ReplyOutboxManualReview, status)
	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit").Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestManualReviewDiscardIsExactlyOnceUnderConcurrency(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, store)

	repo := newTestReplyOutboxRepository(store)
	entry := newReplyOutboxEntry("message:manual-discard-concurrent", 0, `{"body":"stored"}`)
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(ctx, t, repo, entry))

	id := markReplyManualReview(ctx, t, store, entry.MessageID, 0)

	start := make(chan struct{})
	outcomes := make(chan ManualReviewOutcome, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			<-start

			outcome, err := store.DiscardManualReview(ctx, id, testOperatorEmail,
				"incident INC-2026-0818 concurrent decision", testIrisStateOutcomeUnknown)

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

	counts := map[ManualReviewOutcome]int{}

	for outcome := range outcomes {
		counts[outcome]++
	}

	require.Equal(t, 1, counts[ManualReviewDiscarded])
	require.Equal(t, 1, counts[ManualReviewNotManualReview])

	var auditCount int

	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(*) FROM bot_reply_outbox_resolution_audit WHERE outbox_id = ?1", id).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}
