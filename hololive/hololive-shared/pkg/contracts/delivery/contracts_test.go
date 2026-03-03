package delivery_test

import (
	"testing"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	contractsdelivery "github.com/kapu/hololive-shared/pkg/contracts/delivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryOutboxContractConstants(t *testing.T) {
	t.Parallel()

	if contractsdelivery.OutboxPayloadVersionV1 != 1 {
		t.Fatalf("OutboxPayloadVersionV1 = %d", contractsdelivery.OutboxPayloadVersionV1)
	}

	if contractsdelivery.KindMajorEventWeekly != domain.DeliveryKindMajorEventWeekly {
		t.Fatalf("KindMajorEventWeekly = %q", contractsdelivery.KindMajorEventWeekly)
	}
	if contractsdelivery.KindMajorEventMonthly != domain.DeliveryKindMajorEventMonthly {
		t.Fatalf("KindMajorEventMonthly = %q", contractsdelivery.KindMajorEventMonthly)
	}
	if contractsdelivery.KindMemberNewsWeekly != domain.DeliveryKindMemberNewsWeekly {
		t.Fatalf("KindMemberNewsWeekly = %q", contractsdelivery.KindMemberNewsWeekly)
	}
	if contractsdelivery.KindMemberNewsMonthly != domain.DeliveryKindMemberNewsMonthly {
		t.Fatalf("KindMemberNewsMonthly = %q", contractsdelivery.KindMemberNewsMonthly)
	}

	if contractsdelivery.StatusPending != domain.DeliveryStatusPending {
		t.Fatalf("StatusPending = %q", contractsdelivery.StatusPending)
	}
	if contractsdelivery.StatusSent != domain.DeliveryStatusSent {
		t.Fatalf("StatusSent = %q", contractsdelivery.StatusSent)
	}
	if contractsdelivery.StatusFailed != domain.DeliveryStatusFailed {
		t.Fatalf("StatusFailed = %q", contractsdelivery.StatusFailed)
	}
}

func TestOutboxPayloadV1_JSONContract(t *testing.T) {
	t.Parallel()

	payload := contractsdelivery.OutboxPayloadV1{Message: "hello"}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(b) != `{"message":"hello"}` {
		t.Fatalf("json = %s", string(b))
	}

	var decoded contractsdelivery.OutboxPayloadV1
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.Message != "hello" {
		t.Fatalf("decoded.Message = %q", decoded.Message)
	}
}

func TestContentIDContract(t *testing.T) {
	t.Parallel()

	got := contractsdelivery.ContentID("2026-03", "room1")
	if got != "2026-03:room1" {
		t.Fatalf("ContentID = %q", got)
	}
}
