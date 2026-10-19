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
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// Store는 alarm-worker만 쓰는 notification_delivery_outbox의 claim·발송 정산·stale SENDING 격리·보존 정리 저장소다.
// 적재는 hololive-shared delivery.OutboxRepository가 소유하며, 두 저장소는 같은 테이블·status 값·payload 키(message,
// request)를 계약으로 공유한다. 모든 전이가 단일 SQL 문장이라 두 저장소에 걸친 트랜잭션은 없고,
// 적재 rearm(FAILED 전용)과 이 저장소의 전이는 행 잠금과 status·locked_by·attempt_count 조건으로만 직렬화된다. 적재
// rearm은 attempt_count를 0으로 되돌리므로 같은 workerID의 claim 세대는 attempt_count만으로 구분되지 않는다. 그래서
// 이전 claim의 늦은 전이를 막는 불변식은 Dispatcher.processReady의 직렬성 두 가지다. 한 poll 안에서는 이미 처리한 ID
// (processedIDs)를 claim에서 빼므로 같은 poll이 같은 행을 다시 claim하지 않고, poll 사이에서는 한 poll의 모든 항목
// 처리를 join한 뒤에만 다음 poll이 claim한다. 확인 대기 SENT 행의 handoff 전이는 소유자 없이 soft claim 토큰과 요청
// ID로 직렬화된다.
type Store struct {
	pool *pgxpool.Pool
}

const deliveryStatusSending domain.DeliveryOutboxStatus = "SENDING"

const staleSendingFailureReason = "stale sending; external send outcome unknown"

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

	leaseMS, err := positiveMilliseconds("claim lease", lease)
	if err != nil {
		return nil, fmt.Errorf("fetch ready deliveries: %w", err)
	}

	rows, err := r.pool.Query(ctx, claimReadySQL, batchSize, workerID, leaseMS, activeRooms, processedIDs)
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

// ReadySnapshot은 프로세스별 activeRooms/processedIDs 제외 목록이 빈 상태의 durable claimability다.
func (r *Store) ReadySnapshot(ctx context.Context) (workercontract.QueueValues, error) {
	var (
		depth      int64
		ageSeconds float64
	)

	if err := r.pool.QueryRow(ctx, readySnapshotSQL).Scan(&depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("sample ready notification deliveries: %w", err)
	}

	return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(ageSeconds * float64(time.Second))}, nil
}

// 전이 action 이름은 *sendfence.MissError의 Action이자 fence 미스 지표의 action label이다.
const (
	actionMarkSending          = "mark_sending"
	actionMarkSent             = "mark_sent"
	actionMarkQuarantined      = "mark_quarantined"
	actionMarkFailed           = "mark_failed"
	actionMarkRequestExhausted = "mark_request_exhausted"
	actionSaveRequest          = "save_request"
	actionReissueRequest       = "reissue_request"
)

// applyFenced는 sendfence 규약의 단일 행 전이 SQL을 실행하고, 같은 문장이 잠근 행의 관측값으로 적용 여부를 판정한다.
// 인자 args는 SQL 인자 전체이며 $1은 대상 행 id다. 적용되지 않았으면 *sendfence.MissError이며 그 행은 이 문장이
// 바꾸지 않았다.
func (r *Store) applyFenced(ctx context.Context, action string, id int64, query string, args ...any) error {
	if err := r.ensurePool(); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	observed, err := pgx.CollectRows(rows, sendfence.ScanObservation)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	if err := sendfence.Check(action, []int64{id}, observed); err != nil {
		return fmt.Errorf("notification delivery transition: %w", err)
	}

	return nil
}

// MarkSending은 claim 당시 시도 횟수의 PENDING claim을 lease가 살아 있을 때만 SENDING으로 옮기고 sending lease를
// 새로 잡는다. 외부 발송은 이 전이가 적용된 뒤에만 시작한다.
func (r *Store) MarkSending(ctx context.Context, id int64, workerID string, attemptCount int, lease time.Duration) error {
	leaseMS, err := positiveMilliseconds("sending lease", lease)
	if err != nil {
		return fmt.Errorf("%s: %w", actionMarkSending, err)
	}

	return r.applyFenced(ctx, actionMarkSending, id, mustSQL("outbox_repository_0172_04.sql"), id, workerID, attemptCount, leaseMS)
}

// MarkSent는 현재 worker가 claim 당시 시도 횟수로 소유한 SENDING 행만 SENT로 확정한다. 발송이 lease를 넘겨도 성공
// 결과를 잃지 않도록 lease 만료는 보지 않는다. PENDING 행은 발송 전이를 거치지 않았으므로 소유자가 같아도 전이하지 않는다.
// 같은 문장에서 Iris가 admission을 증명한 requestId와 첫 handoff 확인 시각(DB 시각 + min(handoffcheck.FirstCheckDelay,
// handoffHorizon))을 기록하며, 확인이 끝날 때까지 같은 방의 후행 행은 claim되지 않는다. 지평 handoffHorizon은 handoff
// reconciler와 같은 executor attempt_timeout이라 첫 확인도 admission + 지평을 넘지 않는다. 확인할 requestId가
// 없거나 지평이 양수가 아니면 전이하지 않고 오류다.
func (r *Store) MarkSent(ctx context.Context, id int64, workerID string, attemptCount int, irisRequestID string, handoffHorizon time.Duration) error {
	if irisRequestID == "" {
		return fmt.Errorf("%s: iris request id is required", actionMarkSent)
	}

	firstCheckMS, err := positiveMilliseconds("handoff horizon", min(handoffcheck.FirstCheckDelay, handoffHorizon))
	if err != nil {
		return fmt.Errorf("%s: %w", actionMarkSent, err)
	}

	return r.applyFenced(ctx, actionMarkSent, id, mustSQL("outbox_repository_0189_05.sql"),
		id, workerID, attemptCount, irisRequestID, firstCheckMS)
}

// MarkQuarantined는 현재 worker의 같은 claim인 SENDING만 격리하며 자동 재발송 가능한 FAILED로 되돌리지 않습니다.
func (r *Store) MarkQuarantined(ctx context.Context, id int64, workerID string, attemptCount int, reason string) error {
	return r.applyFenced(ctx, actionMarkQuarantined, id, mustSQL("outbox_mark_quarantined.sql"), id, workerID, attemptCount, reason)
}

// MarkFailed는 claim 당시 횟수로 실패 정책을 정하고 동일 횟수·소유권인 행에만 적용합니다.
// 발송 전 lease 만료는 거부하며 발송 후 확정 실패는 기존 SENDING 소유자가 정산할 수 있습니다.
func (r *Store) MarkFailed(ctx context.Context, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string) error {
	return r.markFailed(ctx, actionMarkFailed, id, workerID, attemptCount, maxRetries, backoff, errMsg, false)
}

// markRequestExhausted는 MarkFailed와 같은 소유자·시도 횟수 fence로 행을 종료 FAILED로 끝내고, 같은 문장에서 저장된
// 요청에 exhausted를 세워 producer rearm이 다시 PENDING으로 되살리지 않게 합니다.
func (r *Store) markRequestExhausted(ctx context.Context, id int64, workerID string, attemptCount int, errMsg string) error {
	return r.markFailed(ctx, actionMarkRequestExhausted, id, workerID, attemptCount, 1, 0, errMsg, true)
}

func (r *Store) markFailed(ctx context.Context, action string, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string, exhaustRequest bool) error {
	status, err := deliveryFailureStatus(attemptCount, maxRetries)
	if err != nil {
		return fmt.Errorf("%s policy: %w", action, err)
	}

	return r.applyFenced(ctx, action, id, markFailedSQL,
		id, workerID, attemptCount, status, durationMilliseconds(backoff), errMsg,
		status == domain.DeliveryStatusPending, exhaustRequest,
	)
}

// Cleanup은 보존 기간이 지난 SENT·FAILED·QUARANTINED 행을 배치로 지운다. FAILED/QUARANTINED 항목은 sent_at이 NULL이므로
// created_at을 fallback으로 사용한다. 보존 기한은 각 배치 문장의 DB 시각에서 olderThan을 뺀 값이다.
func (r *Store) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	olderThanMS, err := positiveMilliseconds("cleanup retention", olderThan)
	if err != nil {
		return 0, fmt.Errorf("cleanup deliveries: %w", err)
	}

	out, err := r.cleanupInBatches(ctx, olderThanMS, cleanupBatchSize)
	if err != nil {
		return out, fmt.Errorf("cleanup in batches: %w", err)
	}

	return out, nil
}

func (r *Store) cleanupInBatches(ctx context.Context, olderThanMS int64, batchSize int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}

	var total int64

	for {
		var picked, deleted int64

		if err := r.pool.QueryRow(ctx, mustSQL("outbox_repository_0279_09.sql"), olderThanMS, batchSize).Scan(&picked, &deleted); err != nil {
			return total, fmt.Errorf("delete notification cleanup batch: %w", err)
		}

		total += deleted

		if picked < int64(batchSize) {
			return total, nil
		}

		if err := yieldBetweenCleanupBatches(ctx); err != nil {
			return total, err
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

// QuarantineStaleSending은 sending이 olderThan보다 오래된 SENDING 행을 최대 limit개 결과 불명으로 격리한다.
func (r *Store) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}

	if limit <= 0 {
		return 0, fmt.Errorf("quarantine stale sending: limit must be positive: %d", limit)
	}

	olderThanMS, err := positiveMilliseconds("stale sending threshold", olderThan)
	if err != nil {
		return 0, fmt.Errorf("quarantine stale sending: %w", err)
	}

	tag, err := r.pool.Exec(ctx, mustSQL("outbox_repository_0301_10.sql"), olderThanMS, limit, staleSendingFailureReason)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (r *Store) CountFailed(ctx context.Context) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}

	var count int64

	if err := r.pool.QueryRow(ctx, mustSQL("outbox_repository_0330_11.sql")).Scan(&count); err != nil {
		return 0, fmt.Errorf("count failed: %w", err)
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

// positiveMilliseconds는 양수여야 하는 기간을 SQL 밀리초 인자로 바꾼다. 0 이하는 설정 오류다.
func positiveMilliseconds(name string, value time.Duration) (int64, error) {
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive: %s", name, value)
	}

	return durationMilliseconds(value), nil
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
