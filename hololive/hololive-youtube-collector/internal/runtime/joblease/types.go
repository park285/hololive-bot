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

func (c Config) Validate() error {
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
	if c.MinRetryDelay < 100*time.Millisecond || c.MaxRetryDelay < c.MinRetryDelay || c.MaxRetryDelay > time.Hour {
		return fmt.Errorf("%w: retry delay bounds are invalid", ErrInvalidConfig)
	}
	if c.MinReleaseJitter < 10*time.Millisecond || c.MaxReleaseJitter < c.MinReleaseJitter || c.MaxReleaseJitter > time.Minute {
		return fmt.Errorf("%w: release jitter bounds are invalid", ErrInvalidConfig)
	}
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

func (s JobSpec) validate(contracts sourceobservation.JobContractSet) (sourceobservation.JobContract, []contract.ObservationKind, error) {
	if strings.TrimSpace(s.JobKey) != s.JobKey || s.JobKey == "" || len(s.JobKey) > 512 ||
		strings.TrimSpace(s.CollectionJobKind) != s.CollectionJobKind || s.CollectionJobKind == "" || len(s.CollectionJobKind) > 128 ||
		strings.TrimSpace(s.SubjectKey) != s.SubjectKey || s.SubjectKey == "" || len(s.SubjectKey) > 256 ||
		!s.Provider.Valid() || s.PollInterval < time.Second || s.PollInterval > 24*time.Hour || s.PollInterval%time.Millisecond != 0 {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: identity or poll interval is outside bounds", ErrInvalidJob)
	}
	definition, ok := contracts.Definition(s.CollectionJobKind)
	if !ok || definition.Class != s.Class || definition.FixedSubject != "" && definition.FixedSubject != s.SubjectKey {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: compile-time job contract mismatch", ErrInvalidJob)
	}
	kinds := make([]contract.ObservationKind, 0, len(definition.Emissions))
	for _, emission := range definition.Emissions {
		if emission.Provider == s.Provider {
			kinds = append(kinds, emission.Kind)
		}
	}
	if len(kinds) == 0 {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("%w: provider has no declared emissions", ErrInvalidJob)
	}
	return definition, kinds, nil
}

type Lease interface {
	Proof() contract.LeaseProof
	Renew(ctx context.Context) error
	Complete(ctx context.Context) error
	Defer(ctx context.Context, retryAt time.Time, code string) error
	Release(ctx context.Context) error
}
