package alarm_test

import (
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestAlarmQueueContractConstants(t *testing.T) {
	t.Parallel()

	if contractsalarm.DispatchQueueKey != "alarm:dispatch:queue" {
		t.Fatalf("DispatchQueueKey = %q", contractsalarm.DispatchQueueKey)
	}
	if contractsalarm.NotifyClaimKeyPrefix != "notified:claim:" {
		t.Fatalf("NotifyClaimKeyPrefix = %q", contractsalarm.NotifyClaimKeyPrefix)
	}
	if contractsalarm.NotifyLogicalClaimKeyPrefix != "notified:claim:event:" {
		t.Fatalf("NotifyLogicalClaimKeyPrefix = %q", contractsalarm.NotifyLogicalClaimKeyPrefix)
	}
	if contractsalarm.QueueEnvelopeVersionV1 != 1 {
		t.Fatalf("QueueEnvelopeVersionV1 = %d", contractsalarm.QueueEnvelopeVersionV1)
	}
}

func TestAlarmQueueEnvelopeContract(t *testing.T) {
	t.Parallel()

	env := contractsalarm.AlarmQueueEnvelope{
		Version: contractsalarm.QueueEnvelopeVersionV1,
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
}
