package settlement

import (
	"errors"
	"fmt"
)

var (
	ErrNotRegisteredMember    = errors.New("not registered member")
	ErrNoPendingCycle         = errors.New("no pending cycle")
	ErrInvalidExplicitCycle   = errors.New("invalid explicit cycle")
	ErrFutureCycleNotAllowed  = errors.New("future cycle not allowed")
	ErrCycleNotFoundForMember = errors.New("cycle not found for member")
	ErrNoActiveMembers        = errors.New("no active members")
)

// MultiplePendingCyclesError: 자동 선택이 불가능한 미납 회차가 여러 개인 경우.
type MultiplePendingCyclesError struct {
	CycleKeys []string
}

func (e *MultiplePendingCyclesError) Error() string {
	return fmt.Sprintf("multiple pending cycles: %v", e.CycleKeys)
}
