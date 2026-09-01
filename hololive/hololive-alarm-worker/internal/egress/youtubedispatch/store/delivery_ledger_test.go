package store

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func TestLedgerRecordSentPromotesQuarantineAndPreservesEarliestEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	key := ytcontentid.LogicalKey{
		Kind:      domain.OutboxKindNewVideo,
		LogicalID: "video-ledger-monotonic",
		RoomID:    "room-ledger-monotonic",
	}
	firstObservedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	laterObservedAt := firstObservedAt.Add(2 * time.Minute)

	require.NoError(t, recordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []deliveryLedgerWrite{{
		Key: key, ObservedAt: laterObservedAt, SourceDeliveryID: 20,
	}}))
	require.NoError(t, recordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []deliveryLedgerWrite{{
		Key: key, ObservedAt: firstObservedAt, SourceDeliveryID: 10,
	}}))
	require.NoError(t, recordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []deliveryLedgerWrite{{
		Key: key, ObservedAt: firstObservedAt.Add(time.Minute), SourceDeliveryID: 30,
	}}))
	require.NoError(t, recordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []deliveryLedgerWrite{{
		Key: key, ObservedAt: laterObservedAt.Add(time.Minute), SourceDeliveryID: 40,
	}}))

	record := readDeliveryLedgerRecord(t, pool, key)
	require.Equal(t, LedgerStatusSent, record.Status)
	require.Equal(t, firstObservedAt, record.FirstRecordedAt.UTC())
	require.Equal(t, laterObservedAt, record.UpdatedAt.UTC())
	require.NotNil(t, record.SentAt)
	require.Equal(t, firstObservedAt.Add(time.Minute), record.SentAt.UTC())
	require.NotNil(t, record.QuarantinedAt)
	require.Equal(t, firstObservedAt, record.QuarantinedAt.UTC())
	require.NotNil(t, record.SourceDeliveryID)
	require.Equal(t, int64(30), *record.SourceDeliveryID)
}

func TestCompatibilityTerminalTransactionsRollBackWhenLedgerIdentityIsInvalid(t *testing.T) {
	methods := map[string]func(*DeliveryRepository, int64) error{
		"sent": func(repository *DeliveryRepository, deliveryID int64) error {
			return repository.MarkSentBatch(t.Context(), []int64{deliveryID})
		},
		"quarantined": func(repository *DeliveryRepository, _ int64) error {
			_, _, err := repository.QuarantineStaleSending(t.Context(), time.Minute, 10)

			return err
		},
	}

	for name, run := range methods {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
			lockedAt := time.Now().UTC().Add(-10 * time.Minute)
			deliveryID := seedCompatibilityDelivery(ctx, t, pool, " ", lockedAt)

			if name == "quarantined" {
				_, err := pool.Exec(ctx, `
					UPDATE youtube_notification_delivery
					SET status = $1
					WHERE id = $2
				`, DeliveryStatusSending, deliveryID)
				require.NoError(t, err)
			}

			require.Error(t, run(repository, deliveryID))

			status, _, sentAt := readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
			if name == "sent" {
				require.Equal(t, domain.OutboxStatusPending, status)
			} else {
				require.Equal(t, DeliveryStatusSending, status)
			}
			require.Nil(t, sentAt)
		})
	}
}

func TestCompatibilityCleanupReadyAlwaysFailsClosed(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))

	ready, err := repository.CompatibilityCleanupReady(t.Context())
	require.NoError(t, err)
	require.False(t, ready)
}

func readDeliveryLedgerRecord(t *testing.T, pool *pgxpool.Pool, key ytcontentid.LogicalKey) DeliveryLedgerRecord {
	t.Helper()

	var record DeliveryLedgerRecord
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT kind, logical_id, room_id, status, first_recorded_at, updated_at,
		       sent_at, quarantined_at, source_delivery_id
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, key.Kind, key.LogicalID, key.RoomID).Scan(
		&record.Kind,
		&record.LogicalID,
		&record.RoomID,
		&record.Status,
		&record.FirstRecordedAt,
		&record.UpdatedAt,
		&record.SentAt,
		&record.QuarantinedAt,
		&record.SourceDeliveryID,
	))

	return record
}

func seedCompatibilityDelivery(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	contentID string,
	lockedAt time.Time,
) int64 {
	t.Helper()

	var outboxID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $5)
		RETURNING id
	`, domain.OutboxKindNewVideo, "channel-invalid-ledger", contentID, domain.OutboxStatusPending, lockedAt).Scan(&outboxID))

	var deliveryID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, 0, $4, $4, $4)
		RETURNING id
	`, outboxID, "room-invalid-ledger", domain.OutboxStatusPending, lockedAt).Scan(&deliveryID))

	return deliveryID
}
