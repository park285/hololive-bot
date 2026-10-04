package holodexprovider

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestNewHolodexServiceRejectsInvalidConcurrency(t *testing.T) {
	t.Parallel()

	for _, concurrency := range []int{0, -1} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			t.Parallel()

			cfg := settings.DefaultHolodexOperationalConfig()

			cfg.Concurrency.MaxConcurrentRequests = concurrency

			service, err := NewHolodexServiceWithConfig(&cfg, "https://holodex.example", "test-key", nil, nil, slog.Default())

			if service != nil {
				service.Stop()
				t.Fatal("invalid config created a service")
			}

			if err == nil || !strings.Contains(err.Error(), "MaxConcurrentRequests") {
				t.Fatalf("constructor error = %v, want MaxConcurrentRequests error", err)
			}
		})
	}
}

func TestNewHolodexServiceAcceptsValidRequestConfig(t *testing.T) {
	t.Parallel()

	cfg := settings.DefaultHolodexOperationalConfig()

	cfg.DistributedRateLimit.Enabled = false
	cfg.MaxRetryAttempts = 0

	service, err := NewHolodexServiceWithConfig(&cfg, "https://holodex.example", "test-key", nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("constructor error = %v", err)
	}

	t.Cleanup(service.Stop)

	if service.requester == nil {
		t.Fatal("service has no requester")
	}
}
