package dispatchoutbox

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

var _ Repository = (*PgxRepository)(nil)

type PgxRepository struct {
	pool   *pgxpool.Pool
	now    func() time.Time
	logger *slog.Logger
}

func NewPgxRepository(postgres database.Client, logger *slog.Logger) *PgxRepository {
	if logger == nil {
		logger = slog.Default()
	}
	if postgres == nil {
		return &PgxRepository{now: time.Now, logger: logger}
	}
	return &PgxRepository{pool: postgres.GetPool(), now: time.Now, logger: logger}
}

func NewPgxRepositoryFromPool(pool *pgxpool.Pool, logger *slog.Logger) *PgxRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &PgxRepository{pool: pool, now: time.Now, logger: logger}
}

func expectRowsAffected(got int64, want int, action string) error {
	if got == int64(want) {
		return nil
	}
	return fmt.Errorf("%s: ownership mismatch: updated %d of %d rows", action, got, want)
}

func scanDeliveryRecord(row pgx.Row) (*Record, error) {
	var record Record
	var status string
	var lockedBy *string
	err := row.Scan(
		&record.ID,
		&record.EventID,
		&record.RoomID,
		&record.DedupeKey,
		&record.ClaimKeys,
		&record.DeliveryContext,
		&status,
		&record.AttemptCount,
		&record.NextAttemptAt,
		&lockedBy,
		&record.LockedAt,
		&record.LockExpiresAt,
		&record.SendingStartedAt,
		&record.SentAt,
		&record.DLQAt,
		&record.QuarantinedAt,
		&record.CancelledAt,
		&record.Error,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lockedBy != nil {
		record.LockedBy = *lockedBy
	}
	record.Status = Status(status)
	return &record, nil
}

func idsFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) []int64 {
	ids := make([]int64, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.DispatchOutboxID > 0 {
			ids = append(ids, envelope.DispatchOutboxID)
		}
	}
	return ids
}
