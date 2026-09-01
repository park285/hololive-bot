package store

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func TestTransitionLogicalGroupCompleteSentIsVersionFencedAndAtomic(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	olderID, newerID := seedTransitionLogicalGroup(t, pool, "video-transition-atomic", "room-transition-atomic")
	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, int64(1), claimed[0].RowVersion)
	require.Equal(t, int64(1), claimed[1].RowVersion)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, 1)
	require.Equal(t, olderID, prepared.ActiveRows[0].ID)

	operation, begin, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)
	require.True(t, operation.Valid())

	completed, err := transition.CompleteSent(ctx, operation, nil)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, completed.Outcome)
	require.ElementsMatch(t, []int64{claimed[0].OutboxID, claimed[1].OutboxID}, completed.TouchedOutboxIDs)

	assertTransitionDelivery(t, pool, olderID, domain.OutboxStatusSent, 3, true)
	assertTransitionDelivery(t, pool, newerID, domain.OutboxStatusSent, 3, true)

	var (
		status           LedgerStatus
		sourceDeliveryID int64
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, source_delivery_id
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, domain.OutboxKindNewVideo, "video-transition-atomic", "room-transition-atomic").Scan(&status, &sourceDeliveryID))
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, olderID, sourceDeliveryID)
}

func TestTransitionCompleteSentConfirmsLostCommitResponse(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	deliveryID, _ := seedTransitionLogicalGroup(t, pool, "video-transition-response-lost", "room-transition-response-lost")
	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimPending(ctx, 1)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	operation, result, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	responseLost := errors.New("commit response lost")

	transition.afterCommit = func(operation string) error {
		if operation == "complete sent" {
			return responseLost
		}

		return nil
	}

	completed, err := transition.CompleteSent(ctx, operation, nil)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, completed.Outcome)
	assertTransitionDelivery(t, pool, deliveryID, domain.OutboxStatusSent, 3, true)
}

func TestTransitionQuarantineStaleLogicalGroupIsAllOrNone(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-transition-quarantine", "room-transition-quarantine")
	staleLockedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1, locked_at = $2, row_version = 2
		WHERE id = $3
	`, DeliveryStatusSending, staleLockedAt, ownerID)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)

	result, err := transition.QuarantineStaleLogicalGroups(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, 2, result.QuarantinedDeliveries)
	require.Empty(t, result.Blocked)

	assertTransitionDelivery(t, pool, ownerID, DeliveryStatusQuarantined, 3, false)
	assertTransitionDelivery(t, pool, followerID, DeliveryStatusQuarantined, 1, false)

	var status LedgerStatus

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, domain.OutboxKindNewVideo, "video-transition-quarantine", "room-transition-quarantine").Scan(&status))
	require.Equal(t, LedgerStatusQuarantined, status)
}

func TestTransitionFanoutMaterializesCanonicalChildSetAndConfirmsLostResponse(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)
	seedTransitionFanoutOutbox(t, pool, "video-transition-fanout")

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NotNil(t, claimed[0].LockedAt)

	transition.afterCommit = func(operation string) error {
		if operation == "materialize fanout" {
			return errors.New("fanout commit response lost")
		}

		return nil
	}

	result, err := transition.MaterializeFanout(ctx, claimed[0], []string{" room-b ", "room-a", "room-a"})
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, 2, result.DeliveryCount)
	require.False(t, result.NoTargets)

	var rooms []string

	rows, err := pool.Query(ctx, `
		SELECT room_id
		FROM youtube_notification_delivery
		WHERE outbox_id = $1
		ORDER BY room_id
	`, claimed[0].ID)
	require.NoError(t, err)

	rooms, err = pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Equal(t, []string{"room-a", "room-b"}, rooms)

	var lockedAt *time.Time

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT locked_at FROM youtube_notification_outbox WHERE id = $1
	`, claimed[0].ID).Scan(&lockedAt))
	require.Nil(t, lockedAt)
}

func TestTransitionFanoutNoTargetsPersistsTerminalAt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)
	seedTransitionFanoutOutbox(t, pool, "video-transition-no-targets")

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	result, err := transition.MaterializeFanout(ctx, claimed[0], nil)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.True(t, result.NoTargets)

	var (
		status     domain.OutboxStatus
		sentAt     *time.Time
		terminalAt *time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, sent_at, terminal_at
		FROM youtube_notification_outbox
		WHERE id = $1
	`, claimed[0].ID).Scan(&status, &sentAt, &terminalAt))
	require.Equal(t, domain.OutboxStatusSent, status)
	require.NotNil(t, sentAt)
	require.NotNil(t, terminalAt)
}

func TestTransitionFanoutFailureSchedulesVersionFencedRetry(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)
	seedTransitionFanoutOutbox(t, pool, "video-transition-fanout-retry")

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	result, err := transition.ApplyFanoutFailure(ctx, claimed[0], "subscriber lookup failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	var (
		status        domain.OutboxStatus
		attemptCount  int
		nextAttemptAt time.Time
		lockedAt      *time.Time
		terminalAt    *time.Time
		reason        string
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, attempt_count, next_attempt_at, locked_at, terminal_at, error
		FROM youtube_notification_outbox
		WHERE id = $1
	`, claimed[0].ID).Scan(&status, &attemptCount, &nextAttemptAt, &lockedAt, &terminalAt, &reason))
	require.Equal(t, domain.OutboxStatusPending, status)
	require.Equal(t, 1, attemptCount)
	require.Nil(t, lockedAt)
	require.Nil(t, terminalAt)
	require.Equal(t, "subscriber lookup failed", reason)
	require.WithinDuration(t, time.Now().UTC().Add(time.Second), nextAttemptAt, time.Second)
}

func TestTransitionFanoutFailureTerminatesAtRetryLimitAndConfirmsLostResponse(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	outboxID := seedTransitionFanoutOutbox(t, pool, "video-transition-fanout-terminal")
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET attempt_count = 2
		WHERE id = $1
	`, outboxID)
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	transition.afterCommit = func(operation string) error {
		if operation == "apply fanout failure" {
			return errors.New("fanout failure commit response lost")
		}

		return nil
	}

	result, err := transition.ApplyFanoutFailure(ctx, claimed[0], "subscriber lookup failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, CommitConfirmedPost, result.CommitAdjudication)

	var (
		status       domain.OutboxStatus
		attemptCount int
		lockedAt     *time.Time
		terminalAt   *time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, attempt_count, locked_at, terminal_at
		FROM youtube_notification_outbox
		WHERE id = $1
	`, claimed[0].ID).Scan(&status, &attemptCount, &lockedAt, &terminalAt))
	require.Equal(t, domain.OutboxStatusFailed, status)
	require.Equal(t, 3, attemptCount)
	require.Nil(t, lockedAt)
	require.NotNil(t, terminalAt)
}

func TestTransitionReviveFailedLogicalGroupRequiresAbsentLedger(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-transition-revive", "room-transition-revive")
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1, attempt_count = 3, row_version = 1,
		    locked_at = NULL, error = 'retry exhausted'
		WHERE id = ANY($2::bigint[])
	`, domain.OutboxStatusFailed, []int64{ownerID, followerID})
	require.NoError(t, err)

	transition := newTestTransitionStore(t, pool)

	result, err := transition.ReviveFailedLogicalGroups(ctx, time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, 1, result.RevivedLogicalGroups)
	require.Equal(t, 2, result.RevivedDeliveries)
	require.Empty(t, result.Blocked)

	assertTransitionDelivery(t, pool, ownerID, domain.OutboxStatusPending, 2, false)
	assertTransitionDelivery(t, pool, followerID, domain.OutboxStatusPending, 2, false)
}

func TestTransitionCleanupDeletesPhysicalRowsAndRetainsLogicalLedger(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)
	seedTransitionLogicalGroup(t, pool, "video-transition-cleanup", "room-transition-cleanup")

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	operation, _, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)

	_, err = transition.CompleteSent(ctx, operation, nil)
	require.NoError(t, err)

	terminalAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)

	_, err = pool.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET status = $1, terminal_at = $2, sent_at = COALESCE(sent_at, $2)
		WHERE id = ANY($3::bigint[])
	`, domain.OutboxStatusSent, terminalAt, operation.TouchedOutboxIDs())
	require.NoError(t, err)

	cutoff := terminalAt.Add(24 * time.Hour)
	first, err := transition.CleanupTerminalOutboxes(ctx, cutoff, CleanupCursor{}, 1)
	require.NoError(t, err)
	require.Equal(t, 1, first.ExaminedOutboxes)
	require.Equal(t, 1, first.DeletedOutboxes)

	second, err := transition.CleanupTerminalOutboxes(ctx, cutoff, first.NextCursor, 1)
	require.NoError(t, err)
	require.Equal(t, 1, second.ExaminedOutboxes)
	require.Equal(t, 1, second.DeletedOutboxes)

	var physicalCount int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_delivery
		WHERE outbox_id = ANY($1::bigint[])
	`, operation.TouchedOutboxIDs()).Scan(&physicalCount))
	require.Zero(t, physicalCount)

	var ledgerCount int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3 AND status = $4
	`, domain.OutboxKindNewVideo, "video-transition-cleanup", "room-transition-cleanup", LedgerStatusSent).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
}

func TestTransitionCleanupGuardsTerminalOutboxWithActiveLogicalSibling(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)

	terminalDeliveryID, activeDeliveryID := seedTransitionLogicalGroup(
		t, pool, "video-transition-cleanup-guard", "room-transition-cleanup-guard",
	)
	terminalAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1, sent_at = $2, row_version = 1
		WHERE id = $3
	`, domain.OutboxStatusSent, terminalAt, terminalDeliveryID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET status = $1, sent_at = $2, terminal_at = $2
		WHERE id = (SELECT outbox_id FROM youtube_notification_delivery WHERE id = $3)
	`, domain.OutboxStatusSent, terminalAt, terminalDeliveryID)
	require.NoError(t, err)
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{{
		Key: ytcontentid.LogicalKey{
			Kind:      domain.OutboxKindNewVideo,
			LogicalID: "video-transition-cleanup-guard",
			RoomID:    "room-transition-cleanup-guard",
		},
		ObservedAt:       terminalAt,
		SourceDeliveryID: terminalDeliveryID,
	}}))

	transition := newTestTransitionStore(t, pool)
	result, err := transition.CleanupTerminalOutboxes(ctx, terminalAt.Add(24*time.Hour), CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.ExaminedOutboxes)
	require.Zero(t, result.DeletedOutboxes)
	require.Equal(t, 1, result.GuardedOutboxes)

	var terminalOutboxCount, activeDeliveryCount int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_outbox
		WHERE id = (SELECT outbox_id FROM youtube_notification_delivery WHERE id = $1)
	`, terminalDeliveryID).Scan(&terminalOutboxCount))
	require.Equal(t, 1, terminalOutboxCount)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_delivery
		WHERE id = $1 AND status = $2
	`, activeDeliveryID, domain.OutboxStatusPending).Scan(&activeDeliveryCount))
	require.Equal(t, 1, activeDeliveryCount)
}

func TestTransitionCleanupFailsClosedWithoutCompletedLedgerState(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	_, err := transition.CleanupTerminalOutboxes(t.Context(), time.Now().UTC(), CleanupCursor{}, 10)
	require.ErrorContains(t, err, "ledger backfill is not complete")
}

func newTestTransitionStore(t *testing.T, pool *pgxpool.Pool) *TransitionStore {
	t.Helper()

	transition, err := NewTransitionStore(pool, slog.New(slog.DiscardHandler), TransitionConfig{
		MaxRetries: 3, RetryBackoff: time.Second, LockTimeout: time.Minute,
		ClaimFreshnessWindow: time.Hour, LogicalGroupLimit: 10,
	})
	require.NoError(t, err)

	return transition
}

func seedCompletedLedgerState(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_notification_delivery_ledger_state (
			singleton, schema_version, delivery_high_water_id, outbox_high_water_id,
			delivery_cursor_id, delivery_verify_cursor_id, outbox_cursor_id,
			legacy_coverage_start_at, coverage_verified_at, started_at, completed_at, updated_at
		) VALUES (true, $1, 0, 0, 0, 0, 0, $2, $2, $2, $2, $2)
	`, LedgerSchemaVersion, now)
	require.NoError(t, err)
}

func seedTransitionLogicalGroup(t *testing.T, pool *pgxpool.Pool, contentID, roomID string) (int64, int64) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	var ids [2]int64

	for index := range 2 {
		outboxContentID := contentID

		if index == 1 {
			outboxContentID = " " + contentID + " "
		}

		var outboxID int64

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_outbox (
				kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $6)
			RETURNING id
		`, domain.OutboxKindNewVideo, "channel-transition", outboxContentID, domain.OutboxStatusPending, createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&outboxID))

		var deliveryID int64

		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO youtube_notification_delivery (
				outbox_id, room_id, status, attempt_count, next_attempt_at, created_at
			) VALUES ($1, $2, $3, 0, $4, $5)
			RETURNING id
		`, outboxID, roomID, domain.OutboxStatusPending, createdAt, createdAt.Add(time.Duration(index)*time.Second)).Scan(&deliveryID))

		ids[index] = deliveryID
	}

	return ids[0], ids[1]
}

func seedTransitionFanoutOutbox(t *testing.T, pool *pgxpool.Pool, contentID string) int64 {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)

	var outboxID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox (
			kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at
		) VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $5)
		RETURNING id
	`, domain.OutboxKindNewVideo, "channel-transition-fanout", contentID, domain.OutboxStatusPending, now).Scan(&outboxID))

	return outboxID
}

func loadTransitionTestOutboxes(
	t *testing.T,
	pool *pgxpool.Pool,
	deliveries []domain.YouTubeNotificationDelivery,
) map[int64]domain.YouTubeNotificationOutbox {
	t.Helper()

	result := make(map[int64]domain.YouTubeNotificationOutbox, len(deliveries))
	for i := range deliveries {
		var outbox domain.YouTubeNotificationOutbox

		require.NoError(t, pool.QueryRow(t.Context(), `
			SELECT id, kind, channel_id, content_id, payload::text
			FROM youtube_notification_outbox
			WHERE id = $1
		`, deliveries[i].OutboxID).Scan(&outbox.ID, &outbox.Kind, &outbox.ChannelID, &outbox.ContentID, &outbox.Payload))

		result[outbox.ID] = outbox
	}

	return result
}

func assertTransitionDelivery(
	t *testing.T,
	pool *pgxpool.Pool,
	deliveryID int64,
	status domain.OutboxStatus,
	rowVersion int64,
	wantSentAt bool,
) {
	t.Helper()

	var (
		actualStatus   domain.OutboxStatus
		actualVersion  int64
		actualAttempt  int
		actualLockedAt *time.Time
		actualSentAt   *time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, row_version, attempt_count, locked_at, sent_at
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&actualStatus, &actualVersion, &actualAttempt, &actualLockedAt, &actualSentAt))
	require.Equal(t, status, actualStatus)
	require.Equal(t, rowVersion, actualVersion)
	require.Zero(t, actualAttempt)
	require.Nil(t, actualLockedAt)

	if wantSentAt {
		require.NotNil(t, actualSentAt)
	} else {
		require.Nil(t, actualSentAt)
	}
}
