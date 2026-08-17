package sourceobservation

import (
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type RetryScheduleKind string

const (
	RetryScheduleDelay RetryScheduleKind = "DELAY"
	RetryScheduleAt    RetryScheduleKind = "AT"
)

type RetryBounds struct {
	Minimum time.Duration
	Maximum time.Duration
}

type RetrySchedule struct {
	kind  RetryScheduleKind
	delay time.Duration
	at    time.Time
}

type DeferCollectionInput struct {
	state *deferCollectionState
}

type deferCollectionState struct {
	diagnostic contract.FailureDiagnostic
	bounds     RetryBounds
	schedule   RetrySchedule
}

func NewRetryDelaySchedule(delay time.Duration) (RetrySchedule, error) {
	schedule := RetrySchedule{kind: RetryScheduleDelay, delay: delay}
	if err := schedule.Validate(); err != nil {
		return RetrySchedule{}, err
	}
	return schedule, nil
}

func NewRetryAtSchedule(at time.Time) (RetrySchedule, error) {
	if at.IsZero() {
		return RetrySchedule{}, fmt.Errorf("new retry schedule: timestamp is zero")
	}
	schedule := RetrySchedule{kind: RetryScheduleAt, at: at.UTC()}
	if err := schedule.Validate(); err != nil {
		return RetrySchedule{}, err
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
		return DeferCollectionInput{}, err
	}
	return input, nil
}

func (s RetrySchedule) Kind() RetryScheduleKind { return s.kind }
func (s RetrySchedule) Delay() time.Duration    { return s.delay }
func (s RetrySchedule) At() time.Time           { return s.at }

func (s RetrySchedule) Validate() error {
	switch s.kind {
	case RetryScheduleDelay:
		return s.validateDelay()
	case RetryScheduleAt:
		return s.validateAt()
	default:
		return fmt.Errorf("validate retry schedule: unknown kind %q", s.kind)
	}
}

func (s RetrySchedule) validateDelay() error {
	if s.delay <= 0 || !millisecondAligned(s.delay) || !s.at.IsZero() {
		return fmt.Errorf("validate retry schedule: DELAY requires a positive millisecond-aligned delay")
	}
	return nil
}

func (s RetrySchedule) validateAt() error {
	if s.at.IsZero() || s.at.Location() != time.UTC || s.delay != 0 {
		return fmt.Errorf("validate retry schedule: AT requires a UTC timestamp and zero delay")
	}
	return nil
}

func (b RetryBounds) Validate() error {
	if b.Minimum <= 0 || b.Minimum > b.Maximum || b.Maximum > time.Hour {
		return fmt.Errorf("validate retry bounds: require 0 < min <= max <= 1h")
	}
	if !millisecondAligned(b.Minimum) || !millisecondAligned(b.Maximum) {
		return fmt.Errorf("validate retry bounds: durations must be millisecond-aligned")
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
		return fmt.Errorf("validate defer collection input: input is empty")
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
