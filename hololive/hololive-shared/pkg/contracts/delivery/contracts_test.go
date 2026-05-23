// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package delivery_test

import (
	"testing"

	"github.com/park285/hololive-bot/shared-go/pkg/json"

	contractsdelivery "github.com/kapu/hololive-shared/pkg/contracts/delivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryOutboxContractConstants(t *testing.T) {
	t.Parallel()

	if contractsdelivery.OutboxPayloadVersionV1 != 1 {
		t.Fatalf("OutboxPayloadVersionV1 = %d", contractsdelivery.OutboxPayloadVersionV1)
	}

	if string(contractsdelivery.KindMajorEventWeekly) != string(domain.DeliveryKindMajorEventWeekly) {
		t.Fatalf("KindMajorEventWeekly = %q", contractsdelivery.KindMajorEventWeekly)
	}
	if string(contractsdelivery.KindMajorEventMonthly) != string(domain.DeliveryKindMajorEventMonthly) {
		t.Fatalf("KindMajorEventMonthly = %q", contractsdelivery.KindMajorEventMonthly)
	}
	if string(contractsdelivery.KindMemberNewsWeekly) != string(domain.DeliveryKindMemberNewsWeekly) {
		t.Fatalf("KindMemberNewsWeekly = %q", contractsdelivery.KindMemberNewsWeekly)
	}
	if string(contractsdelivery.KindMemberNewsMonthly) != string(domain.DeliveryKindMemberNewsMonthly) {
		t.Fatalf("KindMemberNewsMonthly = %q", contractsdelivery.KindMemberNewsMonthly)
	}

	if string(contractsdelivery.StatusPending) != string(domain.DeliveryStatusPending) {
		t.Fatalf("StatusPending = %q", contractsdelivery.StatusPending)
	}
	if string(contractsdelivery.StatusSent) != string(domain.DeliveryStatusSent) {
		t.Fatalf("StatusSent = %q", contractsdelivery.StatusSent)
	}
	if string(contractsdelivery.StatusFailed) != string(domain.DeliveryStatusFailed) {
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
