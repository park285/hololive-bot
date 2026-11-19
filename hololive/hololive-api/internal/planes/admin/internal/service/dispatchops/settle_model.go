package dispatchops

import (
	"fmt"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

type SettleRequest struct {
	OperatorID string     `json:"operatorId"`
	Reason     string     `json:"reason"`
	Action     string     `json:"action"`
	Targets    []Revision `json:"targets"`
}

func (r SettleRequest) Validate(id string) error {
	if _, err := ParseID(id); err != nil {
		return err
	}

	if (r.Action != "cancel" && r.Action != "quarantine") || r.OperatorID == "" || r.Reason == "" || !validText(r.OperatorID, 128) || !validText(r.Reason, 1024) {
		return fmt.Errorf("settlement action, operator or reason: %w", ErrInvalidInput)
	}

	return validateTargets(r.Targets, id)
}

func validateSettlement(group []Delivery, request SettleRequest) error {
	if len(group) == 0 || len(group) > MaxReplaySize {
		return ErrConflict
	}

	changed := false

	for index := range group {
		item := &group[index]
		if (item.Status != contractsalarm.DispatchStatusDLQ && item.Status != contractsalarm.DispatchStatusQuarantined) ||
			item.SentAt != nil || item.CancelledAt != nil {
			return ErrConflict
		}

		if request.Action == "cancel" || item.Status == contractsalarm.DispatchStatusDLQ {
			changed = true
		}
	}

	if !changed {
		return ErrConflict
	}

	return validateRevisions(group, request.Targets)
}
