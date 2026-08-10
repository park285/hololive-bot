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

// PartialTransitionError는 ownership fence 또는 attempt CAS가 일부 행과 일치하지
// 않았음을 나타낸다. UnappliedIDs의 행은 recovery/quarantine 경로 소유이므로
// 호출자가 재시도하거나 덮어쓰면 안 된다.
type PartialTransitionError struct {
	Action       string
	Updated      int64
	Expected     int64
	UnappliedIDs []int64
}

func (e *PartialTransitionError) Error() string {
	if len(e.UnappliedIDs) > 0 {
		return fmt.Sprintf("%s: partial transition: updated %d of %d rows, unapplied ids %v", e.Action, e.Updated, e.Expected, e.UnappliedIDs)
	}
	return fmt.Sprintf("%s: ownership changed after external send: updated %d of %d rows", e.Action, e.Updated, e.Expected)
}

func (e *PartialTransitionError) UnappliedDeliveryIDs() []int64 {
	return e.UnappliedIDs
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
	var dispatchGroupKey *string
	var sendUnitID *int64
	err := row.Scan(
		&record.ID,
		&record.EventID,
		&record.RoomID,
		&record.DedupeKey,
		&record.ClaimKeys,
		&record.DeliveryContext,
		&dispatchGroupKey,
		&sendUnitID,
		&record.ClientRequestID,
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
		&record.ErrorCode,
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
	if dispatchGroupKey != nil {
		record.DispatchGroupKey = *dispatchGroupKey
	}
	if sendUnitID != nil {
		record.SendUnitID = *sendUnitID
	}
	record.Status = Status(status)
	return &record, nil
}

func idsFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) []int64 {
	ids := make([]int64, 0, len(envelopes))
	for i := range envelopes {
		if envelopes[i].DispatchOutboxID > 0 {
			ids = append(ids, envelopes[i].DispatchOutboxID)
		}
	}
	return ids
}
