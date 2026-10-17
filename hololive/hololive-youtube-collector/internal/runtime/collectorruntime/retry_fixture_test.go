package collectorruntime

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

// providerCooldown은 provider HTTP 계층이 429/503 응답의 Retry-After 헤더로 만드는 cooldown 오류와 같은 값을 만든다.
func providerCooldown(message, retryAfter string) error {
	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), collecterr.ParseRetryAfter(retryAfter, time.Time{}))
}

// cooldownAt은 helper RPC가 돌려주는 retry-at cooldown을 만든다. 유효하지 않은 시각은 픽스처 오류로 실패시킨다.
func cooldownAt(t *testing.T, message string, at time.Time) error {
	t.Helper()

	hint, err := collecterr.NewRetryAtHint(at)
	if err != nil {
		t.Fatalf("NewRetryAtHint(%s): %v", at, err)
	}

	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), hint)
}
