package notificationdelivery

import (
	"cmp"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// testDispatcherConfig는 운영 worker profile의 notification_delivery 값과 같은 테스트 기준값이다.
// 생성자는 기본값을 채우지 않으므로 테스트가 명시 값을 넘긴다.
func testDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		AttemptTimeout:            10 * time.Second,
		BatchSize:                 50,
		MaxConcurrent:             4,
		MaxRetries:                3,
		PollInterval:              30 * time.Second,
		RetryBackoff:              time.Minute,
		CleanupAfter:              7 * 24 * time.Hour,
		CleanupInterval:           time.Hour,
		CleanupEnabled:            true,
		StaleSendingAfter:         deliveryLease,
		StaleSendingSweepInterval: deliveryLease,
		StaleSendingSweepLimit:    defaultStaleSendingSweepLimit,
	}
}

// withTestDispatcherDefaults는 테스트가 관심 있는 필드만 지정할 수 있도록 0인 숫자 필드를 테스트 기준값으로 채운다.
// CleanupEnabled는 지정한 값을 그대로 쓰므로 nil이면 cleanup이 꺼진다.
func withTestDispatcherDefaults(config *DispatcherConfig) *DispatcherConfig {
	base := testDispatcherConfig()

	if config == nil {
		config = &DispatcherConfig{}
	}

	return &DispatcherConfig{
		AttemptTimeout:            cmp.Or(config.AttemptTimeout, base.AttemptTimeout),
		BatchSize:                 cmp.Or(config.BatchSize, base.BatchSize),
		MaxConcurrent:             cmp.Or(config.MaxConcurrent, base.MaxConcurrent),
		MaxRetries:                cmp.Or(config.MaxRetries, base.MaxRetries),
		PollInterval:              cmp.Or(config.PollInterval, base.PollInterval),
		RetryBackoff:              cmp.Or(config.RetryBackoff, base.RetryBackoff),
		CleanupAfter:              cmp.Or(config.CleanupAfter, base.CleanupAfter),
		CleanupInterval:           cmp.Or(config.CleanupInterval, base.CleanupInterval),
		CleanupEnabled:            config.CleanupEnabled,
		StaleSendingAfter:         cmp.Or(config.StaleSendingAfter, base.StaleSendingAfter),
		StaleSendingSweepInterval: cmp.Or(config.StaleSendingSweepInterval, base.StaleSendingSweepInterval),
		StaleSendingSweepLimit:    cmp.Or(config.StaleSendingSweepLimit, base.StaleSendingSweepLimit),
	}
}

func mustNewDispatcher(tb testing.TB, repository deliveryRepository, sender delivery.MessageSender, logger *slog.Logger, config *DispatcherConfig) *Dispatcher {
	tb.Helper()

	dispatcher, err := NewDispatcher(repository, sender, logger, withTestDispatcherDefaults(config))
	if err != nil {
		tb.Fatalf("NewDispatcher() error = %v", err)
	}

	return dispatcher
}
