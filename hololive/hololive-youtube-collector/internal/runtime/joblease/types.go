package joblease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

const (
	MaxAcquisitionBatch = 100
	MaxQueueCapacity    = 10_000
)

var (
	ErrInvalidConfig = errors.New("collection job lease configuration is invalid")
	ErrInvalidJob    = errors.New("collection job is invalid")
	ErrNotAcquired   = errors.New("collection job was not acquired")
)

// Config는 lease 저장소가 쓰는 TTL·갱신·DB 예산과 release jitter, 후보 조회의 방어 상한만 담습니다.
// Worker 수·poll 주기는 scheduler가, 재시도 범위는 executor가 internal/config에서 직접 받습니다.
type Config struct {
	LeaseTTL         time.Duration
	RenewInterval    time.Duration
	RenewTimeout     time.Duration
	DBTimeout        time.Duration
	CleanupTimeout   time.Duration
	MinReleaseJitter time.Duration
	MaxReleaseJitter time.Duration
	AcquisitionBatch int
	QueueCapacity    int
}

func (c *Config) Validate() error {
	if err := c.validateLeaseBudgets(); err != nil {
		return fmt.Errorf("validate lease budgets: %w", err)
	}

	if err := c.validateReleaseJitter(); err != nil {
		return fmt.Errorf("validate release jitter: %w", err)
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

func (c *Config) validateReleaseJitter() error {
	if c.MinReleaseJitter < 10*time.Millisecond || c.MaxReleaseJitter < c.MinReleaseJitter || c.MaxReleaseJitter > time.Minute {
		return fmt.Errorf("%w: release jitter bounds are invalid", ErrInvalidConfig)
	}

	return nil
}

func (c *Config) validateAcquisition() error {
	if c.AcquisitionBatch < 1 || c.AcquisitionBatch > MaxAcquisitionBatch {
		return fmt.Errorf("%w: acquisition batch must be between 1 and %d", ErrInvalidConfig, MaxAcquisitionBatch)
	}

	if c.QueueCapacity < 1 || c.QueueCapacity > MaxQueueCapacity {
		return fmt.Errorf("%w: queue capacity must be between 1 and %d", ErrInvalidConfig, MaxQueueCapacity)
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

func (s *JobSpec) validate(contracts collection.JobContractSet) (collection.JobContract, []contract.ObservationKind, error) {
	if invalidJobSpecIdentity(s) {
		return collection.JobContract{}, nil, fmt.Errorf("%w: identity or poll interval is outside bounds", ErrInvalidJob)
	}

	definition, ok := contracts.Definition(collection.JobID{Provider: s.Provider, Kind: collection.JobKind(s.CollectionJobKind)})
	if !ok || string(definition.Class()) != s.Class ||
		definition.Class() == collection.JobClassGlobal && definition.LeaseSubject() != s.SubjectKey {
		return collection.JobContract{}, nil, fmt.Errorf("%w: compile-time job contract mismatch", ErrInvalidJob)
	}

	kinds := cadenceKindsForProvider(definition, s.Provider)
	if len(kinds) == 0 {
		return collection.JobContract{}, nil, fmt.Errorf("%w: provider has no declared cadence kinds", ErrInvalidJob)
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

func cadenceKindsForProvider(definition collection.JobContract, provider contract.Provider) []contract.ObservationKind {
	if definition.ID().Provider != provider {
		return nil
	}

	return definition.CadenceKinds()
}

type Lease interface {
	Proof() contract.LeaseProof
	Renew(ctx context.Context) error
	CompleteCurrent(ctx context.Context) error
	Defer(ctx context.Context, input collection.DeferCollectionInput) error
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
