package collectorruntime

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func providerCooldown(message, retryAfter string) error {
	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), collecterr.ParseRetryAfter(retryAfter, time.Time{}))
}

func cooldownAt(t *testing.T, message string, at time.Time) error {
	t.Helper()

	hint, err := collecterr.NewRetryAtHint(at)
	if err != nil {
		t.Fatalf("NewRetryAtHint(%s): %v", at, err)
	}

	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), hint)
}
