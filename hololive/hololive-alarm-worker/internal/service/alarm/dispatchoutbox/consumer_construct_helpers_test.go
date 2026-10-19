package dispatchoutbox

import (
	"log/slog"
	"testing"
	"time"
)

// 시험의 격리 기준은 lease에서 계산하지 않는 고정값이며 기본 lease(60s)와 시험이 넘기는 lease보다 길다.
const testConsumerQuarantineThreshold = 5 * time.Minute

// mustNewConsumer는 고정 격리 기준을 먼저 적용하고 호출자 option을 그 뒤에 적용한다.
func mustNewConsumer(tb testing.TB, repository Repository, claimReleaser ClaimKeyReleaser, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	tb.Helper()

	consumer, err := NewConsumer(repository, claimReleaser, logger, append([]ConsumerOption{WithQuarantineThreshold(testConsumerQuarantineThreshold)}, opts...)...)
	if err != nil {
		tb.Fatalf("NewConsumer() error = %v", err)
	}

	return consumer
}
