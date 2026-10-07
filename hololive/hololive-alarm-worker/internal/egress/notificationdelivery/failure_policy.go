package notificationdelivery

import (
	"errors"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	markFailedSQL    = mustSQL("outbox_repository_0209_06.sql")
	reissueFailedSQL = mustSQL("outbox_reissue_failed_request.sql")
)

// claim 당시 횟수로 정책을 결정하고 저장 시 동일 횟수와 소유권을 함께 검증합니다.
// 모든 DB의 attempt_count가 migration 275부터 bigint이므로 int4 범위 상한을 따로 두지 않습니다.
func deliveryFailureStatus(attemptCount, maxRetries int) (domain.DeliveryOutboxStatus, error) {
	if attemptCount < 0 || maxRetries <= 0 {
		return "", errors.New("invalid delivery attempt count or retry limit")
	}

	if attemptCount >= maxRetries-1 {
		return domain.DeliveryStatusFailed, nil
	}

	return domain.DeliveryStatusPending, nil
}
