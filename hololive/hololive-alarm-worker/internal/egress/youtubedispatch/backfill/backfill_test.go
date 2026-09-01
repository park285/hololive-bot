package backfill

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBackfillFixedHighWaterAndResume(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	fixture := seedFixedHighWaterFixture(ctx, t, pool)

	runner, err := New(pool, Options{BatchSize: 2})
	require.NoError(t, err)

	initial, err := runner.Initialize(ctx)
	require.NoError(t, err)

	postHighWaterOutboxID, postHighWaterDeliveryID := seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindNewVideo,
		"post-high-water",
		`{}`,
		domain.OutboxStatusSent,
		domain.OutboxStatusSent,
		new(fixture.baseSentAt.Add(time.Hour)),
	)
	require.Greater(t, postHighWaterOutboxID, initial.OutboxHighWaterID)
	require.Greater(t, postHighWaterDeliveryID, initial.DeliveryHighWaterID)

	partial, err := runner.backfillDeliveryBatch(ctx)
	require.NoError(t, err)
	require.Positive(t, partial.DeliveryCursorID)
	require.Less(t, partial.DeliveryCursorID, partial.DeliveryHighWaterID)

	restarted, err := New(pool, Options{BatchSize: 2})
	require.NoError(t, err)

	result, err := restarted.Run(ctx)
	require.NoError(t, err)
	require.False(t, result.Completed)
	require.Equal(t, initial.DeliveryHighWaterID, result.State.DeliveryHighWaterID)
	require.Equal(t, initial.OutboxHighWaterID, result.State.OutboxHighWaterID)
	require.Equal(t, result.State.DeliveryHighWaterID, result.State.DeliveryCursorID)
	require.Equal(t, result.State.DeliveryHighWaterID, result.State.DeliveryVerifyCursorID)
	require.Equal(t, result.State.OutboxHighWaterID, result.State.OutboxCursorID)

	var ledgerCount int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*)
		FROM youtube_notification_delivery_ledger
	`).Scan(&ledgerCount))
	require.Equal(t, fixture.expectedLedgerCount, ledgerCount)

	var postHighWaterLedgerCount int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*)
		FROM youtube_notification_delivery_ledger
		WHERE source_delivery_id = $1
	`, postHighWaterDeliveryID).Scan(&postHighWaterLedgerCount))
	require.Zero(t, postHighWaterLedgerCount)

	for _, outboxID := range fixture.terminalOutboxIDs {
		require.NotNil(t, readBackfillTerminalAt(ctx, t, pool, outboxID))
	}

	require.Nil(t, readBackfillTerminalAt(ctx, t, pool, fixture.pendingOutboxID))
	require.Nil(t, readBackfillTerminalAt(ctx, t, pool, postHighWaterOutboxID))
}

type fixedHighWaterFixture struct {
	baseSentAt          time.Time
	expectedLedgerCount int
	terminalOutboxIDs   []int64
	pendingOutboxID     int64
}

func seedFixedHighWaterFixture(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
) fixedHighWaterFixture {
	t.Helper()

	baseSentAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	testCases := []struct {
		kind          domain.OutboxKind
		contentID     string
		payload       string
		outboxStatus  domain.OutboxStatus
		deliveryState domain.OutboxStatus
	}{
		{domain.OutboxKindNewVideo, "video-backfill", `{}`, domain.OutboxStatusSent, domain.OutboxStatusSent},
		{domain.OutboxKindNewShort, "short-backfill", `{"canonical_post_id":"short-backfill"}`, domain.OutboxStatusFailed, store.DeliveryStatusQuarantined},
		{domain.OutboxKindLiveStream, "live-backfill", `{}`, domain.OutboxStatusSent, domain.OutboxStatusSent},
		{domain.OutboxKindCommunityPost, "community-backfill", `{"canonical_post_id":"community-backfill"}`, domain.OutboxStatusSent, domain.OutboxStatusSent},
		{domain.OutboxKindMilestone, "milestone-backfill", `{}`, domain.OutboxStatusFailed, store.DeliveryStatusQuarantined},
	}

	terminalOutboxIDs := make([]int64, 0, len(testCases))
	for i, testCase := range testCases {
		sentAt := baseSentAt.Add(time.Duration(i) * time.Minute)
		outboxID, _ := seedBackfillDelivery(
			ctx,
			t,
			pool,
			testCase.kind,
			testCase.contentID,
			testCase.payload,
			testCase.outboxStatus,
			testCase.deliveryState,
			&sentAt,
		)

		terminalOutboxIDs = append(terminalOutboxIDs, outboxID)
	}

	pendingOutboxID, _ := seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindNewVideo,
		"pending-backfill",
		`{}`,
		domain.OutboxStatusPending,
		domain.OutboxStatusPending,
		nil,
	)

	return fixedHighWaterFixture{
		baseSentAt:          baseSentAt,
		expectedLedgerCount: len(testCases),
		terminalOutboxIDs:   terminalOutboxIDs,
		pendingOutboxID:     pendingOutboxID,
	}
}

func TestBackfillInvalidIdentityDoesNotAdvanceCursor(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	sentAt := time.Now().UTC().Add(-time.Hour)

	_, _ = seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindCommunityPost,
		"invalid-community-backfill",
		`{}`,
		domain.OutboxStatusSent,
		domain.OutboxStatusSent,
		&sentAt,
	)

	runner, err := New(pool, Options{BatchSize: 10})
	require.NoError(t, err)

	_, err = runner.Run(ctx)
	require.ErrorContains(t, err, "canonical post id")

	state, stateErr := runner.CurrentState(ctx)
	require.NoError(t, stateErr)
	require.Zero(t, state.DeliveryCursorID)
	require.Zero(t, state.DeliveryVerifyCursorID)
}

func TestBackfillAntiJoinMismatchDoesNotAdvanceVerificationCursor(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	sentAt := time.Now().UTC().Add(-time.Hour)
	_, deliveryID := seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindNewVideo,
		"anti-join-backfill",
		`{}`,
		domain.OutboxStatusSent,
		domain.OutboxStatusSent,
		&sentAt,
	)

	runner, err := New(pool, Options{BatchSize: 10})
	require.NoError(t, err)

	_, err = runner.Initialize(ctx)
	require.NoError(t, err)

	_, err = runner.backfillDeliveryBatch(ctx)
	require.NoError(t, err)

	_, err = runner.backfillOutboxBatch(ctx)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		DELETE FROM youtube_notification_delivery_ledger
		WHERE source_delivery_id = $1
	`, deliveryID)
	require.NoError(t, err)

	_, err = runner.verifyDeliveryBatch(ctx)
	require.ErrorContains(t, err, "anti-join mismatch")

	state, stateErr := runner.CurrentState(ctx)
	require.NoError(t, stateErr)
	require.Zero(t, state.DeliveryVerifyCursorID)
}

func TestBackfillCoverageCompletionRequiresExplicitEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	runner, err := New(pool, Options{})
	require.NoError(t, err)

	result, err := runner.Run(ctx)
	require.NoError(t, err)
	require.False(t, result.Completed)
	require.Nil(t, result.State.CompletedAt)
	require.Nil(t, result.State.LegacyCoverageStartAt)
	require.Nil(t, result.State.CoverageVerifiedAt)

	coverageStart := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	confirmed, err := New(pool, Options{
		LegacyCoverageStartAt:     &coverageStart,
		HistoricalCoverageChecked: true,
	})
	require.NoError(t, err)

	result, err = confirmed.Run(ctx)
	require.NoError(t, err)
	require.True(t, result.Completed)
	require.NotNil(t, result.State.CompletedAt)
	require.NotNil(t, result.State.LegacyCoverageStartAt)
	require.True(t, result.State.LegacyCoverageStartAt.Equal(coverageStart))
	require.NotNil(t, result.State.CoverageVerifiedAt)
}

func TestBackfillHighWaterInitializationIsStable(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	_, _ = seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindNewVideo,
		"initial-high-water",
		`{}`,
		domain.OutboxStatusPending,
		domain.OutboxStatusPending,
		nil,
	)

	runner, err := New(pool, Options{})
	require.NoError(t, err)

	initial, err := runner.Initialize(ctx)
	require.NoError(t, err)

	_, _ = seedBackfillDelivery(
		ctx,
		t,
		pool,
		domain.OutboxKindNewVideo,
		"after-initialization",
		`{}`,
		domain.OutboxStatusPending,
		domain.OutboxStatusPending,
		nil,
	)

	reloaded, err := runner.Initialize(ctx)
	require.NoError(t, err)
	require.Equal(t, initial.DeliveryHighWaterID, reloaded.DeliveryHighWaterID)
	require.Equal(t, initial.OutboxHighWaterID, reloaded.OutboxHighWaterID)
	require.Equal(t, initial.StartedAt, reloaded.StartedAt)
}

func TestBackfillSentTerminalAtUsesLatestKnownEvidence(t *testing.T) {
	fallback := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	outboxSentAt := fallback.Add(time.Minute)
	deliverySentAt := fallback.Add(2 * time.Minute)

	require.Equal(t, fallback, sentTerminalAt(outboxSourceRow{}, fallback))
	require.Equal(t, outboxSentAt, sentTerminalAt(outboxSourceRow{SentAt: &outboxSentAt}, fallback))
	require.Equal(t, deliverySentAt, sentTerminalAt(outboxSourceRow{
		SentAt:               &outboxSentAt,
		LatestDeliverySentAt: &deliverySentAt,
	}, fallback))
}

func seedBackfillDelivery(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	kind domain.OutboxKind,
	contentID string,
	payload string,
	outboxStatus domain.OutboxStatus,
	deliveryStatus domain.OutboxStatus,
	sentAt *time.Time,
) (int64, int64) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	channelID := fmt.Sprintf("channel-%s", contentID)
	roomID := fmt.Sprintf("room-%s", contentID)

	var outboxID int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox (
			kind,
			channel_id,
			content_id,
			payload,
			status,
			attempt_count,
			next_attempt_at,
			created_at,
			sent_at
		) VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $6, $7)
		RETURNING id
	`, kind, channelID, contentID, payload, outboxStatus, createdAt, sentAt).Scan(&outboxID))

	var deliveryID int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery (
			outbox_id,
			room_id,
			status,
			attempt_count,
			next_attempt_at,
			created_at,
			sent_at
		) VALUES ($1, $2, $3, 0, $4, $4, $5)
		RETURNING id
	`, outboxID, roomID, deliveryStatus, createdAt, sentAt).Scan(&deliveryID))

	return outboxID, deliveryID
}

func readBackfillTerminalAt(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	outboxID int64,
) *time.Time {
	t.Helper()

	var terminalAt *time.Time

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT terminal_at
		FROM youtube_notification_outbox
		WHERE id = $1
	`, outboxID).Scan(&terminalAt))

	return terminalAt
}

//go:fix inline
