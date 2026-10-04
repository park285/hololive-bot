package dispatchops

import (
	"errors"
	"testing"
	"time"
)

const settlementFailureStatus = "dlq"

func TestSettlementValidation(t *testing.T) {
	now := time.Now()
	request := SettleRequest{OperatorID: "operator", Reason: "발송 보류", Action: "cancel", Targets: []Revision{{ID: "1", UpdatedAt: now}}}

	if err := request.Validate("1"); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"", "retry", "delete"} {
		invalid := request

		invalid.Action = action

		if !errors.Is(invalid.Validate("1"), ErrInvalidInput) {
			t.Fatal(action)
		}
	}

	for _, status := range statuses {
		group := []Delivery{{ID: "1", Status: status, UpdatedAt: now}}
		err := validateSettlement(group, request)
		allowed := status == settlementFailureStatus || status == "quarantined"

		if (err == nil) != allowed {
			t.Fatalf("status %s: %v", status, err)
		}
	}

	for _, sent := range []bool{true, false} {
		item := Delivery{ID: "1", Status: settlementFailureStatus, UpdatedAt: now}

		if sent {
			item.SentAt = &now
		} else {
			item.CancelledAt = &now
		}

		if !errors.Is(validateSettlement([]Delivery{item}, request), ErrConflict) {
			t.Fatalf("terminal marker accepted: sent=%v", sent)
		}
	}

	request.Action = "quarantine"
	if !errors.Is(validateSettlement([]Delivery{{ID: "1", Status: "quarantined", UpdatedAt: now}}, request), ErrConflict) {
		t.Fatal("already quarantined")
	}
}
