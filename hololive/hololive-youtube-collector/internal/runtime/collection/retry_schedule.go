package collection

import (
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type RetryBounds struct {
	Minimum time.Duration
	Maximum time.Duration
}

// RetrySchedule은 UTC 절대 시각을 보관하며 최종 지연 범위는 DB 시계로 보정합니다.
type RetrySchedule struct {
	at time.Time
}

type DeferCollectionInput struct {
	state *deferCollectionState
}

type deferCollectionState struct {
	diagnostic contract.FailureDiagnostic
	bounds     RetryBounds
	schedule   RetrySchedule
}

// NewRetryAtSchedule은 시각을 UTC로 정규화하며 영 시각은 거부합니다.
func NewRetryAtSchedule(at time.Time) (RetrySchedule, error) {
	if at.IsZero() {
		return RetrySchedule{}, errors.New("new retry schedule: timestamp is zero")
	}

	schedule := RetrySchedule{at: at.UTC()}
	if err := schedule.Validate(); err != nil {
		return RetrySchedule{}, fmt.Errorf("validate: %w", err)
	}

	return schedule, nil
}

func NewDeferCollectionInput(
	diagnostic contract.FailureDiagnostic,
	bounds RetryBounds,
	schedule RetrySchedule,
) (DeferCollectionInput, error) {
	input := DeferCollectionInput{state: &deferCollectionState{
		diagnostic: diagnostic,
		bounds:     bounds,
		schedule:   schedule,
	}}
	if err := input.Validate(); err != nil {
		return DeferCollectionInput{}, fmt.Errorf("validate: %w", err)
	}

	return input, nil
}

// At은 저장소에 전달할 UTC 재시도 시각을 반환합니다.
func (s RetrySchedule) At() time.Time { return s.at }

// Validate는 생성자를 거치지 않은 빈 값이나 UTC가 아닌 시각을 거부합니다.
func (s RetrySchedule) Validate() error {
	if s.at.IsZero() || s.at.Location() != time.UTC {
		return errors.New("validate retry schedule: requires a nonzero UTC timestamp")
	}

	return nil
}

func (b RetryBounds) Validate() error {
	if b.Minimum <= 0 || b.Minimum > b.Maximum || b.Maximum > time.Hour {
		return errors.New("validate retry bounds: require 0 < min <= max <= 1h")
	}

	if !millisecondAligned(b.Minimum) || !millisecondAligned(b.Maximum) {
		return errors.New("validate retry bounds: durations must be millisecond-aligned")
	}

	return nil
}

func (d DeferCollectionInput) Diagnostic() contract.FailureDiagnostic {
	if d.state == nil {
		return contract.FailureDiagnostic{}
	}

	return d.state.diagnostic
}

func (d DeferCollectionInput) Bounds() RetryBounds {
	if d.state == nil {
		return RetryBounds{}
	}

	return d.state.bounds
}

func (d DeferCollectionInput) Schedule() RetrySchedule {
	if d.state == nil {
		return RetrySchedule{}
	}

	return d.state.schedule
}

func (d DeferCollectionInput) Validate() error {
	if d.state == nil {
		return errors.New("validate defer collection input: input is empty")
	}

	if err := d.state.diagnostic.ValidateFor(contract.TerminalDefer); err != nil {
		return fmt.Errorf("validate defer collection input: %w", err)
	}

	if err := d.state.bounds.Validate(); err != nil {
		return fmt.Errorf("validate defer collection input: %w", err)
	}

	if err := d.state.schedule.Validate(); err != nil {
		return fmt.Errorf("validate defer collection input: %w", err)
	}

	return nil
}

func millisecondAligned(value time.Duration) bool {
	return value%time.Millisecond == 0
}
