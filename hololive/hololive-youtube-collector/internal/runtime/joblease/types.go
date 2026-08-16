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
	LeaseTTL            time.Duration
	RenewInterval       time.Duration
	ProviderTimeout     time.Duration
	NormalizationBudget time.Duration
	PublishBudget       time.Duration
	MinRetryDelay       time.Duration
	MaxRetryDelay       time.Duration
	MinReleaseJitter    time.Duration
	MaxReleaseJitter    time.Duration
	AcquisitionBatch    int
	WorkerCount         int
	QueueCapacity       int
	PollCadence         time.Duration
}

func (c *Config) Validate() error {
	if err := c.validateLeaseBudgets(); err != nil {
		return err
	}
	if err := c.validateRetryJitter(); err != nil {
		return err
	}
	return c.validateAcquisition()
}

func (c *Config) validateLeaseBudgets() error {
	if c.LeaseTTL < time.Second || c.LeaseTTL > 30*time.Minute {
		return fmt.Errorf("%w: lease TTL must be between 1 second and 30 minutes", ErrInvalidConfig)
	}
	if c.ProviderTimeout <= 0 || c.NormalizationBudget <= 0 || c.PublishBudget <= 0 ||
		c.LeaseTTL <= c.ProviderTimeout+c.NormalizationBudget+c.PublishBudget {
		return fmt.Errorf("%w: lease TTL must exceed provider, normalization, and publish budgets", ErrInvalidConfig)
	}
	if c.RenewInterval <= 0 || c.RenewInterval > c.LeaseTTL/3 {
		return fmt.Errorf("%w: renew interval must be positive and at most one third of lease TTL", ErrInvalidConfig)
	}
	return nil
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
	definition, ok := contracts.Definition(s.CollectionJobKind)
	if !ok || definition.Class != s.Class || definition.FixedSubject != "" && definition.FixedSubject != s.SubjectKey {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: compile-time job contract mismatch", ErrInvalidJob)
	}
	kinds := emissionsForProvider(definition, s.Provider)
	if len(kinds) == 0 {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: provider has no declared emissions", ErrInvalidJob)
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

func emissionsForProvider(definition sourceobservation.JobContract, provider contract.Provider) []contract.ObservationKind {
	kinds := make([]contract.ObservationKind, 0, len(definition.Emissions))
	for _, emission := range definition.Emissions {
		if emission.Provider == provider {
			kinds = append(kinds, emission.Kind)
		}
	}
	return kinds
}

type Lease interface {
	Proof() contract.LeaseProof
	Renew(ctx context.Context) error
	Complete(ctx context.Context) error
	Defer(ctx context.Context, retryAt time.Time, code, class, detail string) error
	Release(ctx context.Context) error
}
