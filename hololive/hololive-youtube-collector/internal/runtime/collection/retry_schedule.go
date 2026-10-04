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

type retryScheduleKind uint8

const (
	retryBounded retryScheduleKind = iota + 1
	retryNotBefore
)

// RetrySchedule은 UTC 재시도 시각과 일반 backoff/명시적 cooldown 하한을 구분합니다.
// 두 경로 모두 DB 시계의 최소 대기를 적용하며 일반 backoff에만 최대 대기를 적용합니다.
type RetrySchedule struct {
	at   time.Time
	kind retryScheduleKind
}

type DeferCollectionInput struct {
	state *deferCollectionState
}

type deferCollectionState struct {
	diagnostic contract.FailureDiagnostic
	bounds     RetryBounds
	schedule   RetrySchedule
}

// NewRetryAtSchedule은 DB 시계의 RetryBounds 안으로 보정할 일반 backoff를 만듭니다.
func NewRetryAtSchedule(at time.Time) (RetrySchedule, error) {
	return newRetrySchedule(at, retryBounded)
}

// NewRetryNotBeforeSchedule은 provider가 명시한 cooldown 하한을 보존합니다.
// DeferCollectionInput은 cooldown/COOLDOWN 진단에만 이 일정을 허용합니다.
// PostgreSQL 저장 정밀도 아래의 나노초는 조기 재시도를 막도록 마이크로초로 올림합니다.
func NewRetryNotBeforeSchedule(at time.Time) (RetrySchedule, error) {
	if rounded := at.Truncate(time.Microsecond); rounded.Before(at) {
		at = rounded.Add(time.Microsecond)
	}

	return newRetrySchedule(at, retryNotBefore)
}

func newRetrySchedule(at time.Time, kind retryScheduleKind) (RetrySchedule, error) {
	if at.IsZero() {
		return RetrySchedule{}, errors.New("new retry schedule: timestamp is zero")
	}

	schedule := RetrySchedule{at: at.UTC(), kind: kind}
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

// IsNotBefore는 명시적 cooldown의 하한을 로컬 최대 대기로 줄이지 않아야 함을 알립니다.
func (s RetrySchedule) IsNotBefore() bool { return s.kind == retryNotBefore }

// Validate는 빈 값, 알 수 없는 종류와 UTC가 아닌 시각을 거부합니다.
func (s RetrySchedule) Validate() error {
	if s.at.IsZero() || s.at.Location() != time.UTC {
		return errors.New("validate retry schedule: requires a nonzero UTC timestamp")
	}

	if s.kind != retryBounded && s.kind != retryNotBefore {
		return errors.New("validate retry schedule: unknown schedule kind")
	}

	if s.kind == retryNotBefore && (s.at.Year() < 1 || s.at.Year() > 9999 || s.at.Nanosecond()%1000 != 0) {
		return errors.New("validate retry schedule: not-before requires a representable microsecond timestamp in years 1..9999")
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

	if d.state.schedule.IsNotBefore() && (d.state.diagnostic.Code() != contract.ErrorCooldown ||
		d.state.diagnostic.Class() != contract.ClassCooldown) {
		return errors.New("validate defer collection input: not-before requires cooldown/COOLDOWN diagnostic")
	}

	return nil
}

func millisecondAligned(value time.Duration) bool {
	return value%time.Millisecond == 0
}
