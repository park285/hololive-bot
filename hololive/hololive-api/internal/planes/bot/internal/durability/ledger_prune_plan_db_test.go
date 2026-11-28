package durability

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prunePlanRetainedRows = 20000
	prunePlanExpiredRows  = 5
	prunePlanBatchSize    = 100
)

func TestDurablePruneUsesTerminalIndexesInsteadOfScanningRetainedHistory(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	seedPrunePlanLedgers(ctx, t, store)

	now := time.Now().UnixMilli()
	terminalCutoff := now - (8 * 24 * time.Hour).Milliseconds()
	manualReviewCutoff := now - (30 * 24 * time.Hour).Milliseconds()

	for _, tc := range []struct {
		relation string
		query    string
		args     []any
	}{
		{relation: "bot_webhook_inbox", query: durablePruneInboxSQL, args: []any{terminalCutoff, prunePlanBatchSize}},
		{relation: "bot_command_executions", query: durablePruneCommandSQL, args: []any{terminalCutoff, prunePlanBatchSize}},
		{relation: "bot_reply_outbox", query: durablePruneOutboxSQL, args: []any{terminalCutoff, manualReviewCutoff, prunePlanBatchSize}},
	} {
		t.Run(tc.relation, func(t *testing.T) {
			details := explainQueryPlan(ctx, t, store, tc.query, tc.args...)

			for _, detail := range details {
				assert.NotEqual(t, "SCAN "+tc.relation, detail, "prune must not scan retained %s history", tc.relation)
			}

			assert.True(t, usesNamedIndex(details, tc.relation), "prune must search %s through an index: %v", tc.relation, details)
		})
	}
}

func seedPrunePlanLedgers(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	now := time.Now()
	expired, retained := now.Add(-9*24*time.Hour).UnixMilli(), now.Add(-24*time.Hour).UnixMilli()

	statements := []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?1)
		INSERT INTO bot_webhook_inbox (message_id, room_id, ordering_key, payload, status, available_at, created_at, updated_at)
		SELECT 'message:prune-plan-' || i, 'room-1', 'room:prune-plan', '{}', 'succeeded', ?4, ?4,
		       CASE WHEN i <= ?2 THEN ?3 ELSE ?4 END
		FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?1)
		INSERT INTO bot_command_executions (message_id, command_kind, status, claim_token, claimed_at, completed_at, created_at, updated_at)
		SELECT 'message:prune-plan-' || i, 'webhook', 'succeeded', 'plan-token', ?4, ?4, ?4,
		       CASE WHEN i <= ?2 THEN ?3 ELSE ?4 END
		FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?1)
		INSERT INTO bot_reply_outbox (message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id, status,
		                              available_at, created_at, updated_at)
		SELECT 'message:prune-plan-' || i, 'final', 0, 'room-1',
		       CASE WHEN i % 10 = 0 THEN '{"kind":"text","message":"review"}' END,
		       printf('%.64c', 'a'), printf('prune-plan-%08d', i),
		       CASE WHEN i % 10 = 0 THEN 'manual_review' ELSE 'handoff_completed' END,
		       ?4, ?4, CASE WHEN i <= ?2 THEN ?3 ELSE ?4 END
		FROM n`,
	}

	for i, statement := range statements {
		_, err := store.Exec(ctx, statement, prunePlanRetainedRows+prunePlanExpiredRows, prunePlanExpiredRows, expired, retained)
		require.NoError(t, err, "seed prune plan statement %d", i)
	}

	_, err := store.Exec(ctx, "ANALYZE")
	require.NoError(t, err)
}

func explainQueryPlan(ctx context.Context, t *testing.T, store *Store, query string, args ...any) []string {
	t.Helper()

	rows, err := store.Query(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)

	defer rows.Close()

	var details []string

	for rows.Next() {
		var (
			id, parent, unused int64
			detail             string
		)

		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))

		details = append(details, detail)
	}

	require.NoError(t, rows.Err())
	require.NotEmpty(t, details)

	return details
}

func usesNamedIndex(details []string, relation string) bool {
	for _, detail := range details {
		if strings.HasPrefix(detail, "SEARCH "+relation+" USING INDEX ") || strings.HasPrefix(detail, "SEARCH "+relation+" USING COVERING INDEX ") {
			return true
		}
	}

	return false
}
