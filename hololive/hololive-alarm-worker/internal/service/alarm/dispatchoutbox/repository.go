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
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

var _ Repository = (*PgxRepository)(nil)

type PgxRepository struct {
	pool   *pgxpool.Pool
	now    func() time.Time
	logger *slog.Logger
}

// fence 미스 오류의 Action이자 지표 action 라벨이다.
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

// applyFenced는 sendfence 관측 열을 돌려주는 전이 문장을 실행하고 want 대상 전원이 적용됐는지 확인한다. 미스는
// 대상별 원인을 지표·로그로 남기고 *sendfence.MissError를 그대로 돌려준다. 적용되지 않은 대상은 이 문장이 바꾸지
// 않았으므로 호출자는 소유권이 남은 원인(sendfence.Reason.OwnershipHeld)일 때만 그 행에 후속 전이를 쓸 수 있다.
func (r *PgxRepository) applyFenced(ctx context.Context, querier fencedQuerier, action, query string, want []int64, args ...any) error {
	observed, err := queryFenceObservations(ctx, querier, action, query, args...)
	if err != nil {
		return err
	}

	return r.checkFenced(action, want, observed)
}

// queryFenceObservations는 sendfence 관측 열을 돌려주는 전이 문장을 실행하고 관측 행을 읽는다.
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

// checkFenced는 want 대상 전원이 적용됐는지 확인하고, 미스면 대상별 원인을 지표·로그로 남긴다.
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

// fencedTargetIDs는 전이 입력의 delivery ID를 입력 순서로 돌려준다.
func fencedTargetIDs[T fencedTarget](targets []T) []int64 {
	ids := make([]int64, 0, len(targets))
	for i := range targets {
		ids = append(ids, targets[i].deliveryID())
	}

	return ids
}

func scanDeliveryRecord(row pgx.Row) (*Record, error) {
	var (
		record           Record
		status           string
		lockedBy         *string
		dispatchGroupKey *string
		sendUnitID       *int64
	)

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
		return nil, fmt.Errorf("scan: %w", err)
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

// claimsFromEnvelopes는 drain이 복원한 envelope의 claim을 돌려준다. 각 attempt는 claim 시 행의 attempt_count이며
// attachRecordMetadata가 0보다 클 때만 Retry로 옮겨 둔다.
func claimsFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) []DeliveryClaim {
	claims := make([]DeliveryClaim, 0, len(envelopes))
	for i := range envelopes {
		if envelopes[i].DispatchOutboxID > 0 {
			claims = append(claims, DeliveryClaim{ID: envelopes[i].DispatchOutboxID, AttemptCount: envelopeAttempt(&envelopes[i])})
		}
	}

	return claims
}

func envelopeAttempt(envelope *domain.AlarmQueueEnvelope) int {
	if envelope.Retry == nil {
		return 0
	}

	return envelope.Retry.Attempt
}
