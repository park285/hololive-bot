package botruntime

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestDurableSettlementTimeoutLeavesShutdownHeadroom(t *testing.T) {
	t.Parallel()

	if durableSettlementTimeout <= 0 {
		t.Fatal("settlement timeout must be positive")
	}
	if durableSettlementTimeout > constants.AppTimeout.Shutdown/2 {
		t.Fatalf("durableSettlementTimeout %s must stay within half of AppTimeout.Shutdown %s: "+
			"Stop joins in-flight settlement via wg.Wait and every plane shares one shutdown context sequentially",
			durableSettlementTimeout, constants.AppTimeout.Shutdown)
	}
}
