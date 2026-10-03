package youtubedispatch

import (
	"cmp"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
)

// testDispatchConfig는 운영 worker profile의 youtube_delivery 값과 같은 테스트 기준값이다.
// 생성자는 기본값을 채우지 않으므로 테스트가 명시 값을 넘긴다.
func testDispatchConfig() dispatchstate.Config {
	return dispatchstate.Config{
		BatchSize:                   50,
		LockTimeout:                 5 * time.Minute,
		PollInterval:                2 * time.Second,
		MaxRetries:                  3,
		RetryBackoff:                time.Minute,
		CleanupAfter:                7 * 24 * time.Hour,
		CleanupEnabled:              true,
		ReviveEnabled:               true,
		ReviveInterval:              5 * time.Minute,
		ReviveFreshnessWindow:       time.Hour,
		ClaimFreshnessWindow:        2 * time.Hour,
		DeliveryParallelism:         4,
		DeliverySendTimeout:         10 * time.Second,
		SubscriberLookupParallelism: 16,
		AggregateSyncInterval:       30 * time.Second,
		TelemetryPollInterval:       30 * time.Second,
		TelemetryFlushBatch:         200,
		TelemetryRetryBackoff:       30 * time.Second,
		TelemetryRetention:          24 * time.Hour,
	}
}

// withTestDispatchConfigDefaults는 테스트가 관심 있는 필드만 지정할 수 있도록 0인 숫자·기간 필드를 기준값으로 채운다.
// 지정한 bool 필드는 그대로 쓰고, claim 신선도 기간은 지정한 revive 기간과 주기의 합 이상으로 맞춘다.
func withTestDispatchConfigDefaults(config *dispatchstate.Config) *dispatchstate.Config {
	base := testDispatchConfig()

	if config == nil {
		return &base
	}

	out := dispatchstate.Config{
		BatchSize:                   cmp.Or(config.BatchSize, base.BatchSize),
		LockTimeout:                 cmp.Or(config.LockTimeout, base.LockTimeout),
		PollInterval:                cmp.Or(config.PollInterval, base.PollInterval),
		MaxRetries:                  cmp.Or(config.MaxRetries, base.MaxRetries),
		RetryBackoff:                cmp.Or(config.RetryBackoff, base.RetryBackoff),
		CleanupAfter:                config.CleanupAfter,
		CleanupEnabled:              config.CleanupEnabled,
		ReviveEnabled:               config.ReviveEnabled,
		ReviveInterval:              cmp.Or(config.ReviveInterval, base.ReviveInterval),
		ReviveFreshnessWindow:       cmp.Or(config.ReviveFreshnessWindow, base.ReviveFreshnessWindow),
		ClaimFreshnessWindow:        cmp.Or(config.ClaimFreshnessWindow, base.ClaimFreshnessWindow),
		DeliveryParallelism:         cmp.Or(config.DeliveryParallelism, base.DeliveryParallelism),
		DeliverySendTimeout:         cmp.Or(config.DeliverySendTimeout, base.DeliverySendTimeout),
		SubscriberLookupParallelism: cmp.Or(config.SubscriberLookupParallelism, base.SubscriberLookupParallelism),
		AggregateSyncInterval:       cmp.Or(config.AggregateSyncInterval, base.AggregateSyncInterval),
		TelemetryPollInterval:       cmp.Or(config.TelemetryPollInterval, base.TelemetryPollInterval),
		TelemetryFlushBatch:         cmp.Or(config.TelemetryFlushBatch, base.TelemetryFlushBatch),
		TelemetryRetryBackoff:       cmp.Or(config.TelemetryRetryBackoff, base.TelemetryRetryBackoff),
		TelemetryRetention:          cmp.Or(config.TelemetryRetention, base.TelemetryRetention),
	}

	out.ClaimFreshnessWindow = max(out.ClaimFreshnessWindow, out.ReviveFreshnessWindow+out.ReviveInterval)

	return &out
}
