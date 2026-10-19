package notificationdelivery

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	markFailedSQL    = mustSQL("outbox_repository_0209_06.sql")
	reissueFailedSQL = mustSQL("outbox_reissue_failed_request.sql")
	saveRequestSQL   = mustSQL("outbox_save_request.sql")
)

// fence 미스의 처리 결과다. 각 값은 소유권이 남은 요청 미스에 소유자 fence가 걸린 실패 전이 적용(failed), 행을
// 건드리지 않음(fence_lost), 그 실패 전이 자체의 DB 오류(transition_error)를 뜻한다.
const (
	fenceMissResultFailed          = "failed"
	fenceMissResultFenceLost       = "fence_lost"
	fenceMissResultTransitionError = "transition_error"
)

var notificationDeliveryFenceMissTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_notification_delivery_fence_miss_total",
	Help: "Notification delivery owner transitions rejected by the row fence, by transition action, observed reason, and settlement result.",
}, []string{"action", "reason", "result"})

// claim 당시 횟수로 정책을 결정하고 저장 시 동일 횟수와 소유권을 함께 검증합니다.
// 모든 DB의 attempt_count가 migration 275부터 bigint이므로 int4 범위 상한을 따로 두지 않습니다.
func deliveryFailureStatus(attemptCount, maxRetries int) (domain.DeliveryOutboxStatus, error) {
	if attemptCount < 0 || maxRetries <= 0 {
		return "", errors.New("invalid delivery attempt count or retry limit")
	}

	if !retryBudgetLeft(attemptCount, maxRetries) {
		return domain.DeliveryStatusFailed, nil
	}

	return domain.DeliveryStatusPending, nil
}

// retryBudgetLeft는 claim 당시 attemptCount인 행이 실패를 한 번 더 기록한 뒤에도 다시 시도할 수 있는지 보고한다.
// 실패 전이·409 재발급·handoff failed 재발급이 모두 attempt_count를 1 올리고 이 기준으로 재시도와 종료를 가른다.
func retryBudgetLeft(attemptCount, maxRetries int) bool {
	return attemptCount < maxRetries-1
}
