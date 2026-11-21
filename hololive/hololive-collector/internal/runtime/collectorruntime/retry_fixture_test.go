package collectorruntime

import (
	"testing"
	"time"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func providerCooldown(message, retryAfter string) error {
	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), collecterr.ParseRetryAfter(retryAfter))
}

func cooldownAt(t *testing.T, message string, at time.Time) error {
	t.Helper()

	hint, err := collecterr.NewRetryAtHint(at)
	if err != nil {
		t.Fatalf("NewRetryAtHint(%s): %v", at, err)
	}

	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), hint)
}
