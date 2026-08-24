package joblease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

const (
	MaxAcquisitionBatch = 100
	MaxWorkerCount      = 64
	MaxQueueCapacity    = 10_000
)

var (
	ErrInvalidConfig   = errors.New("collection job lease configuration is invalid")
	ErrInvalidJob      = errors.New("collection job is invalid")
	ErrNotAcquired     = errors.New("collection job was not acquired")
	ErrFenceLost       = sourceobservation.ErrCollectionFenceLost
	ErrProjectionStale = sourceobservation.ErrProjectionStale
	ErrTargetDisabled  = sourceobservation.ErrTargetDisabled
)

type Config struct {
	LeaseTTL         time.Duration
	RenewInterval    time.Duration
	RenewTimeout     time.Duration
	DBTimeout        time.Duration
	CleanupTimeout   time.Duration
	MinRetryDelay    time.Duration
	MaxRetryDelay    time.Duration
	MinReleaseJitter time.Duration
	MaxReleaseJitter time.Duration
	AcquisitionBatch int
	WorkerCount      int
	QueueCapacity    int
	PollCadence      time.Duration
}

func (c *Config) Validate() error {
	if err := c.validateLeaseBudgets(); err != nil {
		return fmt.Errorf("validate lease budgets: %w", err)
	}

	if err := c.validateRetryJitter(); err != nil {
		return fmt.Errorf("validate retry jitter: %w", err)
	}

	if err := c.validateAcquisition(); err != nil {
		return fmt.Errorf("validate acquisition: %w", err)
	}

	return nil
}

func (c *Config) validateLeaseBudgets() error {
	if c.LeaseTTL < time.Second || c.LeaseTTL > 30*time.Minute {
		return fmt.Errorf("%w: lease TTL must be between 1 second and 30 minutes", ErrInvalidConfig)
	}

	if invalidRenewTimeout(c.RenewTimeout) || invalidRuntimeTimeout(c.DBTimeout) || invalidRuntimeTimeout(c.CleanupTimeout) {
		return fmt.Errorf("%w: renew, database, or cleanup timeout is outside bounds", ErrInvalidConfig)
	}

	if invalidRenewBudget(c.RenewInterval, c.RenewTimeout, c.LeaseTTL) {
		return fmt.Errorf("%w: renew interval and timeout do not fit the lease TTL", ErrInvalidConfig)
	}

	return nil
}

func invalidRenewTimeout(timeout time.Duration) bool {
	return timeout <= 0 || timeout > time.Minute
}

func invalidRuntimeTimeout(timeout time.Duration) bool {
	return timeout < 100*time.Millisecond || timeout > time.Minute
}

func invalidRenewBudget(interval, timeout, ttl time.Duration) bool {
	return interval <= 0 || interval >= ttl || interval+timeout+time.Second >= ttl
}

func (c *Config) validateRetryJitter() error {
	if c.MinRetryDelay < 100*time.Millisecond || c.MaxRetryDelay < c.MinRetryDelay || c.MaxRetryDelay > time.Hour {
		return fmt.Errorf("%w: retry delay bounds are invalid", ErrInvalidConfig)
	}

	if c.MinReleaseJitter < 10*time.Millisecond || c.MaxReleaseJitter < c.MinReleaseJitter || c.MaxReleaseJitter > time.Minute {
		return fmt.Errorf("%w: release jitter bounds are invalid", ErrInvalidConfig)
	}

	return nil
}

func (c *Config) validateAcquisition() error {
	if c.AcquisitionBatch < 1 || c.AcquisitionBatch > MaxAcquisitionBatch {
		return fmt.Errorf("%w: acquisition batch must be between 1 and %d", ErrInvalidConfig, MaxAcquisitionBatch)
	}

	if c.WorkerCount < 1 || c.WorkerCount > MaxWorkerCount {
		return fmt.Errorf("%w: worker count must be between 1 and %d", ErrInvalidConfig, MaxWorkerCount)
	}

	if c.QueueCapacity < c.WorkerCount || c.QueueCapacity > MaxQueueCapacity {
		return fmt.Errorf("%w: queue capacity must be between worker count and %d", ErrInvalidConfig, MaxQueueCapacity)
	}

	if c.PollCadence < 100*time.Millisecond || c.PollCadence > time.Minute {
		return fmt.Errorf("%w: poll cadence must be between 100 milliseconds and 1 minute", ErrInvalidConfig)
	}

	return nil
}

type JobSpec struct {
	JobKey            string
	Provider          contract.Provider
	Class             string
	CollectionJobKind string
	SubjectKey        string
	PollInterval      time.Duration
}

func (s *JobSpec) validate(contracts sourceobservation.JobContractSet) (sourceobservation.JobContract, []contract.ObservationKind, error) {
	if invalidJobSpecIdentity(s) {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: identity or poll interval is outside bounds", ErrInvalidJob)
	}

	definition, ok := contracts.Definition(sourceobservation.JobID{Provider: s.Provider, Kind: sourceobservation.JobKind(s.CollectionJobKind)})
	if !ok || string(definition.Class()) != s.Class ||
		definition.Class() == sourceobservation.JobClassGlobal && definition.LeaseSubject() != s.SubjectKey {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: compile-time job contract mismatch", ErrInvalidJob)
	}

	kinds := cadenceKindsForProvider(definition, s.Provider)
	if len(kinds) == 0 {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: provider has no declared cadence kinds", ErrInvalidJob)
	}

	return definition, kinds, nil
}

func invalidJobSpecIdentity(s *JobSpec) bool {
	return invalidBoundedToken(s.JobKey, 512) ||
		invalidBoundedToken(s.CollectionJobKind, 128) ||
		invalidBoundedToken(s.SubjectKey, 256) ||
		!s.Provider.Valid() ||
		invalidPollInterval(s.PollInterval)
}

func invalidBoundedToken(value string, maxLength int) bool {
	return strings.TrimSpace(value) != value || value == "" || len(value) > maxLength
}

func invalidPollInterval(interval time.Duration) bool {
	return interval < time.Second || interval > 24*time.Hour || interval%time.Millisecond != 0
}

func cadenceKindsForProvider(definition sourceobservation.JobContract, provider contract.Provider) []contract.ObservationKind {
	if definition.ID().Provider != provider {
		return nil
	}

	return definition.CadenceKinds()
}

type Lease interface {
	Proof() contract.LeaseProof
	Renew(ctx context.Context) error
	CompleteCurrent(ctx context.Context) error
	Defer(ctx context.Context, retryAt time.Time, code, class, detail string) error
	Release(ctx context.Context, reason ReleaseReason) error
}

type ReleaseReason contract.CollectionErrorCode

const (
	ReleaseShutdown   ReleaseReason = ReleaseReason(contract.ErrorShutdownRelease)
	ReleaseSuperseded ReleaseReason = ReleaseReason(contract.ErrorSupersededRelease)
	ReleaseRenewFail  ReleaseReason = ReleaseReason(contract.ErrorRenewFailedRelease)
)

func (r ReleaseReason) Valid() bool {
	return contract.CollectionErrorCode(r).Releasable()
}

func (r ReleaseReason) ErrorCode() contract.CollectionErrorCode {
	return contract.CollectionErrorCode(r)
}

type RetryDecisionKind string

const (
	RetryDecisionDelay RetryDecisionKind = "DELAY"
	RetryDecisionAt    RetryDecisionKind = "AT"
)

type RetryDecision struct {
	kind  RetryDecisionKind
	delay time.Duration
	at    time.Time
}

func NewRetryDelay(delay time.Duration) (RetryDecision, error) {
	decision := RetryDecision{kind: RetryDecisionDelay, delay: delay}
	if err := decision.Validate(); err != nil {
		return decision, fmt.Errorf("validate: %w", err)
	}

	return decision, nil
}

func NewRetryAt(at time.Time) (RetryDecision, error) {
	decision := RetryDecision{kind: RetryDecisionAt, at: at.UTC()}
	if err := decision.Validate(); err != nil {
		return decision, fmt.Errorf("validate: %w", err)
	}

	return decision, nil
}

func (d RetryDecision) Kind() RetryDecisionKind { return d.kind }
func (d RetryDecision) Delay() (time.Duration, bool) {
	return d.delay, d.kind == RetryDecisionDelay
}
func (d RetryDecision) At() (time.Time, bool) { return d.at, d.kind == RetryDecisionAt }
func (d RetryDecision) Validate() error {
	if d.kind == RetryDecisionDelay && d.delay > 0 && d.at.IsZero() {
		return nil
	}

	if d.kind == RetryDecisionAt && !d.at.IsZero() && d.delay == 0 {
		return nil
	}

	return fmt.Errorf("%w: retry decision is invalid", ErrInvalidJob)
}
