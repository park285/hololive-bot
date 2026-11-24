package youtubedispatch

import (
	"fmt"
	"strings"
	"time"
)

type Config struct {
	BatchSize                   int
	LockTimeout                 time.Duration
	PollInterval                time.Duration
	MaxRetries                  int
	RetryBackoff                time.Duration
	CleanupAfter                time.Duration
	CleanupEnabled              bool
	ReviveEnabled               bool
	ReviveInterval              time.Duration
	ReviveFreshnessWindow       time.Duration
	ClaimFreshnessWindow        time.Duration
	DeliveryParallelism         int
	DeliverySendTimeout         time.Duration
	SubscriberLookupParallelism int
	AggregateSyncInterval       time.Duration
	HandoffHorizon              time.Duration
}

const minSendingFinalizeGrace = 5 * time.Second

func (c *Config) sendLeaseBudget() time.Duration {
	return c.DeliverySendTimeout + max(2*c.PollInterval, minSendingFinalizeGrace)
}

func (c *Config) Validate() error {
	required := []struct {
		name  string
		valid bool
	}{
		{"batch size", c.BatchSize > 0},
		{"lock timeout", c.LockTimeout > 0},
		{"poll interval", c.PollInterval > 0},
		{"max retries", c.MaxRetries > 0},
		{"retry backoff", c.RetryBackoff > 0},
		{"revive interval", c.ReviveInterval > 0},
		{"revive freshness window", c.ReviveFreshnessWindow > 0},
		{"claim freshness window", c.ClaimFreshnessWindow > 0},
		{"delivery parallelism", c.DeliveryParallelism > 0},
		{"delivery send timeout", c.DeliverySendTimeout > 0},
		{"subscriber lookup parallelism", c.SubscriberLookupParallelism > 0},
		{"aggregate sync interval", c.AggregateSyncInterval > 0},
		{"handoff horizon", c.HandoffHorizon > 0},
	}

	var invalid []string

	for _, setting := range required {
		if !setting.valid {
			invalid = append(invalid, setting.name)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("settings must be positive: %s", strings.Join(invalid, ", "))
	}

	if minimum := c.ReviveFreshnessWindow + c.ReviveInterval; c.ClaimFreshnessWindow < minimum {
		return fmt.Errorf("claim freshness window %s must be at least revive freshness window plus revive interval (%s)", c.ClaimFreshnessWindow, minimum)
	}

	if budget := c.sendLeaseBudget(); c.LockTimeout < budget {
		return fmt.Errorf("lock timeout %s must be at least the send lease budget (%s)", c.LockTimeout, budget)
	}

	return nil
}
