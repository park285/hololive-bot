package dispatchops

import "fmt"

// SettleRequest는 외부 발송 없이 실패 묶음을 취소하거나 격리합니다.
// OperatorID는 인증된 gateway 세션에서만 결합합니다.
type SettleRequest struct {
	OperatorID string     `json:"operatorId"`
	Reason     string     `json:"reason"`
	Action     string     `json:"action"`
	Targets    []Revision `json:"targets"`
}

// Validate는 감사 정보와 명시적인 처리 종류·리비전을 검사합니다.
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
		if (item.Status != "dlq" && item.Status != "quarantined") || item.SentAt != nil || item.CancelledAt != nil {
			return ErrConflict
		}

		if request.Action == "cancel" || item.Status == "dlq" {
			changed = true
		}
	}

	if !changed {
		return ErrConflict
	}

	return validateRevisions(group, request.Targets)
}
