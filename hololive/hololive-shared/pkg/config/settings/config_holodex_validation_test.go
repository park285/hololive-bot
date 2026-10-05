package settings

import (
	"strings"
	"testing"
	"time"
)

func TestValidateHolodexRequestConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		mutate    func(*HolodexConfig)
		wantError string
	}{
		{name: "defaults", mutate: func(*HolodexConfig) {}},
		{name: "no retries or pacing", mutate: func(c *HolodexConfig) { c.MaxRetryAttempts = 0; c.Concurrency.RequestDelay = 0 }},
		{name: "maximum retries", mutate: func(c *HolodexConfig) { c.MaxRetryAttempts = MaxHolodexRetryAttempts }},
		{name: "zero concurrency", mutate: func(c *HolodexConfig) { c.Concurrency.MaxConcurrentRequests = 0 }, wantError: "MaxConcurrentRequests"},
		{name: "negative concurrency", mutate: func(c *HolodexConfig) { c.Concurrency.MaxConcurrentRequests = -1 }, wantError: "MaxConcurrentRequests"},
		{name: "negative retries", mutate: func(c *HolodexConfig) { c.MaxRetryAttempts = -1 }, wantError: "MaxRetryAttempts"},
		{name: "excess retries", mutate: func(c *HolodexConfig) { c.MaxRetryAttempts = 10 }, wantError: "MaxRetryAttempts"},
		{name: "negative delay", mutate: func(c *HolodexConfig) { c.Concurrency.RequestDelay = -time.Second }, wantError: "RequestDelay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := DefaultHolodexOperationalConfig()
			tc.mutate(&cfg)

			err := ValidateHolodexRequestConfig(&cfg)

			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateHolodexRequestConfig() = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %s", err, tc.wantError)
			}
		})
	}
}
