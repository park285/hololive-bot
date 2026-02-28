package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// outboxPayload: outbox에 저장되는 메시지 payload
type outboxPayload struct {
	Message string `json:"message"`
}

// OutboxRepository: notification delivery outbox DB 연산
type OutboxRepository struct {
	db     *gorm.DB
	logger *slog.Logger
}

// NewOutboxRepository: OutboxRepository 생성
func NewOutboxRepository(db *gorm.DB, logger *slog.Logger) *OutboxRepository {
	return &OutboxRepository{db: db, logger: logger}
}

// Enqueue: outbox 항목 삽입. PENDING 중복은 무시, FAILED는 재시도로 갱신.
func (r *OutboxRepository) Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error {
	payload, _ := json.Marshal(outboxPayload{Message: message})
	contentID := periodKey + ":" + roomID

	sql := `INSERT INTO notification_delivery_outbox (kind, period_key, room_id, content_id, payload, status, attempt_count, next_attempt_at)
            VALUES (?, ?, ?, ?, ?, 'PENDING', 0, NOW())
            ON CONFLICT (kind, content_id) DO UPDATE
            SET payload = EXCLUDED.payload, status = 'PENDING', attempt_count = 0, next_attempt_at = NOW(), error = NULL
            WHERE notification_delivery_outbox.status = 'FAILED'`

	result := r.db.WithContext(ctx).Exec(sql, kind, periodKey, roomID, contentID, string(payload))
	if result.Error != nil {
		return fmt.Errorf("enqueue delivery: %w", result.Error)
	}
	return nil
}

// FetchAndLock: PENDING 항목을 배치로 가져오며 원자적으로 locked_at 갱신 (FOR UPDATE SKIP LOCKED)
func (r *OutboxRepository) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	lockExpiry := time.Now().Add(-lockTimeout)
	now := time.Now()

	var items []domain.NotificationDeliveryOutbox
	sql := `WITH claim AS (
        SELECT id FROM notification_delivery_outbox
        WHERE status = 'PENDING'
          AND (locked_at IS NULL OR locked_at < ?)
          AND next_attempt_at <= ?
        ORDER BY created_at ASC LIMIT ?
        FOR UPDATE SKIP LOCKED
    )
    UPDATE notification_delivery_outbox o SET locked_at = ?
    FROM claim WHERE o.id = claim.id
    RETURNING o.*`

	err := r.db.WithContext(ctx).Raw(sql, lockExpiry, now, batchSize, now).Scan(&items).Error
	if err != nil {
		return nil, fmt.Errorf("fetch and lock: %w", err)
	}
	return items, nil
}

// MarkSent: 발송 완료 상태로 갱신
func (r *OutboxRepository) MarkSent(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&domain.NotificationDeliveryOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":  domain.DeliveryStatusSent,
			"sent_at": time.Now(),
			"error":   nil,
		}).Error
}

// MarkFailed: 실패 처리. maxRetries 초과 시 FAILED, 미만이면 PENDING 유지 + backoff 적용.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error {
	now := time.Now()
	sql := `UPDATE notification_delivery_outbox
            SET attempt_count = attempt_count + 1,
                error = ?,
                status = CASE WHEN attempt_count + 1 >= ? THEN 'FAILED' ELSE 'PENDING' END,
                next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
                locked_at = NULL
            WHERE id = ?`

	return r.db.WithContext(ctx).Exec(sql, errMsg, maxRetries, maxRetries, now.Add(backoff), id).Error
}

// Cleanup: 오래된 SENT/FAILED 항목 삭제
// FAILED 항목은 sent_at이 NULL이므로 created_at을 fallback으로 사용
func (r *OutboxRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result := r.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.DeliveryStatusSent, domain.DeliveryStatusFailed, cutoff).
		Delete(&domain.NotificationDeliveryOutbox{})
	return result.RowsAffected, result.Error
}

// CountByStatus: 특정 상태의 항목 수 조회
func (r *OutboxRepository) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.NotificationDeliveryOutbox{}).
		Where("status = ?", status).Count(&count).Error
	return count, err
}
