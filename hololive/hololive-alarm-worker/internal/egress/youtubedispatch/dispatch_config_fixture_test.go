package youtubedispatch

import (
	"cmp"
	"time"
)

func testDispatchConfig() Config {
	return Config{
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
		HandoffHorizon:              5 * time.Minute,
	}
}

func withTestDispatchConfigDefaults(config *Config) *Config {
	base := testDispatchConfig()

	if config == nil {
		return &base
	}

	out := Config{
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
		HandoffHorizon:              cmp.Or(config.HandoffHorizon, base.HandoffHorizon),
	}

	out.ClaimFreshnessWindow = max(out.ClaimFreshnessWindow, out.ReviveFreshnessWindow+out.ReviveInterval)
	out.LockTimeout = max(out.LockTimeout, out.sendLeaseBudget())

	return &out
}
