package delivery

import (
	"errors"
	"math"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	markFailedSQL    = mustSQL("outbox_repository_0209_06.sql")
	reissueFailedSQL = mustSQL("outbox_reissue_failed_request.sql")
)

// claim 당시 횟수로 정책을 결정하고 저장 시 동일 횟수와 소유권을 함께 검증합니다.
func deliveryFailureStatus(attemptCount, maxRetries int) (domain.DeliveryOutboxStatus, error) {
	if attemptCount < 0 || attemptCount >= math.MaxInt32 || maxRetries <= 0 {
		return "", errors.New("invalid delivery attempt count or retry limit")
	}

	if attemptCount >= maxRetries-1 {
		return domain.DeliveryStatusFailed, nil
	}

	return domain.DeliveryStatusPending, nil
}
