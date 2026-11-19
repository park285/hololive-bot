package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

var _ Repository = (*PgxRepository)(nil)

type PgxRepository struct {
	pool   *pgxpool.Pool
	now    func() time.Time
	logger *slog.Logger
}

const (
	fenceActionLoadSendRequest      = "load_send_request"
	fenceActionPinSendRequest       = "pin_send_request"
	fenceActionBeginSendRequest     = "begin_send_request"
	fenceActionReissueSendRequest   = "reissue_send_request"
	fenceActionMarkSent             = "mark_sent"
	fenceActionRouteFailures        = "route_failures"
	fenceActionRouteSendingFailures = "route_sending_failures"
	fenceActionRequeuePreSend       = "requeue_pre_send"
	fenceActionReleaseLeased        = "release_leased"
	fenceActionMoveToDLQ            = "move_to_dlq"
	fenceActionQuarantine           = "quarantine"
)

type fencedQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PoolSource interface {
	GetPool() *pgxpool.Pool
}

func NewPgxRepository(postgres PoolSource, logger *slog.Logger) (*PgxRepository, error) {
	if postgres == nil {
		return nil, errors.New("new dispatch outbox repository: postgres is required")
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &PgxRepository{pool: postgres.GetPool(), now: time.Now, logger: logger}, nil
}

func (r *PgxRepository) applyFenced(ctx context.Context, querier fencedQuerier, action, query string, want []int64, args ...any) error {
	observed, err := queryFenceObservations(ctx, querier, action, query, args...)
	if err != nil {
		return err
	}

	return r.checkFenced(action, want, observed)
}

func queryFenceObservations(ctx context.Context, querier fencedQuerier, action, query string, args ...any) ([]sendfence.Observation, error) {
	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}

	observed, err := pgx.CollectRows(rows, sendfence.ScanObservation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}

	return observed, nil
}

func (r *PgxRepository) checkFenced(action string, want []int64, observed []sendfence.Observation) error {
	checkErr := sendfence.Check(action, want, observed)
	if checkErr == nil {
		return nil
	}

	if miss, ok := errors.AsType[*sendfence.MissError](checkErr); ok {
		observePGFenceMiss(miss)
		r.logger.Warn("dispatch delivery send fence miss",
			slog.String("action", action),
			slog.Int("rows_applied", miss.Applied),
			slog.Int("rows_expected", miss.Expected),
			slog.Any("error", miss),
		)
	}

	return fmt.Errorf("apply fenced dispatch transition: %w", checkErr)
}

type fencedTarget interface {
	deliveryID() int64
}

func (c DeliveryClaim) deliveryID() int64  { return c.ID }
func (u FailureUpdate) deliveryID() int64  { return u.ID }
func (u TerminalUpdate) deliveryID() int64 { return u.ID }

func fencedTargetIDs[T fencedTarget](targets []T) []int64 {
	ids := make([]int64, 0, len(targets))
	for i := range targets {
		ids = append(ids, targets[i].deliveryID())
	}

	return ids
}

func scanDeliveryRecord(row pgx.Row) (*Record, error) {
	var (
		record   Record
		status   string
		lockedBy *string
	)

	err := row.Scan(
		&record.ID,
		&record.EventID,
		&record.RoomID,
		&record.DedupeKey,
		&record.ClaimKeys,
		&record.DeliveryContext,
		&record.DispatchGroupKey,
		&record.SendUnitID,
		&record.ClientRequestID,
		&record.RequestUnpinned,
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
		return nil, fmt.Errorf("scan: %w", err)
	}

	if lockedBy != nil {
		record.LockedBy = *lockedBy
	}

	record.Status = Status(status)

	return &record, nil
}

func claimsFromEnvelopes(envelopes []AlarmQueueEnvelope) []DeliveryClaim {
	claims := make([]DeliveryClaim, 0, len(envelopes))
	for i := range envelopes {
		if envelopes[i].DispatchOutboxID > 0 {
			claims = append(claims, DeliveryClaim{ID: envelopes[i].DispatchOutboxID, AttemptCount: envelopeAttempt(&envelopes[i])})
		}
	}

	return claims
}

func envelopeAttempt(envelope *AlarmQueueEnvelope) int {
	if envelope.Retry == nil {
		return 0
	}

	return envelope.Retry.Attempt
}
