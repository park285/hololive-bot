package notificationdelivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// Store는 alarm-worker만 쓰는 notification_delivery_outbox의 claim·발송 정산·stale SENDING 격리·보존 정리 저장소다.
// 적재는 hololive-shared delivery.OutboxRepository가 소유하며, 두 저장소는 같은 테이블·status 값·payload 키(message,
// request, known_unsent)를 계약으로 공유한다. 모든 전이가 단일 SQL 문장이라 두 저장소에 걸친 트랜잭션은 없고,
// 적재 rearm(FAILED 전용)과 이 저장소의 전이는 행 잠금과 status·locked_by·attempt_count 조건으로만 직렬화된다.
type Store struct {
	pool *pgxpool.Pool
}

const deliveryStatusSending domain.DeliveryOutboxStatus = "SENDING"

// 결과 불명(stale SENDING)을 FAILED로 회수하면 rearm(outbox_enqueue_batch_upsert.sql의
// WHERE status='FAILED')이 재발송해 중복 노출 위험이 있어 별도 terminal 상태로 격리한다.
const deliveryStatusQuarantined domain.DeliveryOutboxStatus = "QUARANTINED"

const staleSendingFailureReason = "stale sending; external send outcome unknown"

const defaultStaleSendingSweepLimit = 100

const (
	cleanupBatchSize  = 1000
	cleanupBatchYield = 10 * time.Millisecond
)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// fetchReadyAndLock은 lease가 없거나 만료된 PENDING 행 중 활성 방을 제외하고 각 방의 첫 due 항목만 임대합니다.
// 다른 worker가 선행 항목을 처리 중이어도 후행 항목이 앞서 발송되지 않습니다. 현재 writer는 locked_at·locked_by·
// lock_expires_at을 항상 함께 쓰고 함께 비운다.
func (r *Store) fetchReadyAndLock(ctx context.Context, workerID string, batchSize int, lease time.Duration, activeRooms []string, processedIDs []int64) ([]domain.NotificationDeliveryOutbox, error) {
	if err := r.ensurePool(); err != nil {
		return nil, fmt.Errorf("fetch ready deliveries: %w", err)
	}

	rows, err := r.pool.Query(ctx, mustSQL("outbox_claim_ready.sql"), batchSize, workerID, positiveDurationMilliseconds(lease), activeRooms, processedIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch ready deliveries: %w", err)
	}
	defer rows.Close()

	items, err := pgx.CollectRows(rows, scanNotificationDeliveryOutbox)
	if err != nil {
		return nil, fmt.Errorf("collect ready deliveries: %w", err)
	}

	return items, nil
}

func (r *Store) MarkSending(ctx context.Context, id int64, workerID string, lease time.Duration) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0172_04.sql"),
		deliveryStatusSending, positiveDurationMilliseconds(lease),
		id, domain.DeliveryStatusPending, workerID,
	)
	if err != nil {
		return false, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

func (r *Store) MarkSent(ctx context.Context, id int64, workerID string) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0189_05.sql"),
		domain.DeliveryStatusSent, id, domain.DeliveryStatusPending, deliveryStatusSending, workerID,
	)
	if err != nil {
		return false, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// MarkQuarantined는 현재 worker의 SENDING만 격리하며 자동 재발송 가능한 FAILED로 되돌리지 않습니다.
func (r *Store) MarkQuarantined(ctx context.Context, id int64, workerID, reason string) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("quarantine delivery: %w", err)
	}

	tag, err := r.pool.Exec(ctx, mustSQL("outbox_mark_quarantined.sql"), id, workerID, reason)
	if err != nil {
		return false, fmt.Errorf("quarantine delivery: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// MarkFailed는 claim 당시 횟수로 실패 정책을 정하고 동일 횟수·소유권인 행에만 적용합니다.
// 발송 전 lease 만료는 거부하며 발송 후 확정 실패는 기존 SENDING 소유자가 정산할 수 있습니다.
func (r *Store) MarkFailed(ctx context.Context, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	status, err := deliveryFailureStatus(attemptCount, maxRetries)
	if err != nil {
		return false, fmt.Errorf("mark failed policy: %w", err)
	}

	tag, err := r.pool.Exec(ctx, markFailedSQL,
		errMsg, status, durationMilliseconds(backoff), id,
		domain.DeliveryStatusPending, deliveryStatusSending, workerID, attemptCount, status == domain.DeliveryStatusPending,
	)
	if err != nil {
		return false, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// Cleanup은 보존 기간이 지난 SENT·FAILED·QUARANTINED 행을 배치로 지운다. FAILED/QUARANTINED 항목은 sent_at이 NULL이므로
// created_at을 fallback으로 사용한다.
func (r *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	out, err := r.cleanupInBatches(ctx, time.Now().Add(-olderThan), cleanupBatchSize)
	if err != nil {
		return out, fmt.Errorf("cleanup in batches: %w", err)
	}

	return out, nil
}

func (r *Store) cleanupInBatches(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
	}

	var total int64

	for {
		tag, err := r.pool.Exec(ctx,
			mustSQL("outbox_repository_0279_09.sql"),
			domain.DeliveryStatusSent, domain.DeliveryStatusFailed, deliveryStatusQuarantined, cutoff, batchSize,
		)
		if err != nil {
			return total, fmt.Errorf("exec: %w", err)
		}

		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(batchSize) {
			return total, nil
		}

		if err := yieldBetweenCleanupBatches(ctx); err != nil {
			return total, fmt.Errorf("yield between cleanup batches: %w", err)
		}
	}
}

func yieldBetweenCleanupBatches(ctx context.Context) error {
	if retry.Sleep(ctx, cleanupBatchYield) {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("yield between cleanup batches: %w", err)
	}

	return nil
}

func (r *Store) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
	}

	if limit <= 0 {
		limit = defaultStaleSendingSweepLimit
	}

	if olderThan <= 0 {
		olderThan = deliveryLease
	}

	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0301_10.sql"),
		deliveryStatusSending, positiveDurationMilliseconds(olderThan),
		limit, deliveryStatusQuarantined, staleSendingFailureReason,
	)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (r *Store) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
	}

	var count int64

	if err := r.pool.QueryRow(ctx, mustSQL("outbox_repository_0330_11.sql"), status).Scan(&count); err != nil {
		return 0, fmt.Errorf("count by status: %w", err)
	}

	return count, nil
}

func durationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}

	if milliseconds := value.Milliseconds(); milliseconds > 0 {
		return milliseconds
	}

	return 1
}

func positiveDurationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		value = deliveryLease
	}

	if milliseconds := durationMilliseconds(value); milliseconds > 0 {
		return milliseconds
	}

	return 1
}

func (r *Store) ensurePool() error {
	if r == nil || r.pool == nil {
		return errors.New("notification delivery store: postgres pool is required")
	}

	return nil
}

func scanNotificationDeliveryOutbox(row pgx.CollectableRow) (domain.NotificationDeliveryOutbox, error) {
	var (
		item     domain.NotificationDeliveryOutbox
		kind     string
		status   string
		payload  []byte
		lockedAt sql.NullTime
		sentAt   sql.NullTime
		errText  sql.NullString
	)

	err := row.Scan(
		&item.ID,
		&kind,
		&item.PeriodKey,
		&item.RoomID,
		&item.ContentID,
		&payload,
		&status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&lockedAt,
		&sentAt,
		&errText,
	)
	if err != nil {
		return domain.NotificationDeliveryOutbox{}, fmt.Errorf("scan: %w", err)
	}

	item.Kind = domain.DeliveryOutboxKind(kind)
	item.Payload = string(payload)
	item.Status = domain.DeliveryOutboxStatus(status)
	item.LockedAt = lockedAt
	item.SentAt = sentAt
	item.Error = errText

	return item, nil
}
