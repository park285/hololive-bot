package collectorruntime

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

type ExecutionProfile struct {
	maxUpstreamCalls int
	requestTimeout   time.Duration
	rateInterval     time.Duration
	providerInflight int
	overhead         time.Duration
	collectTimeout   time.Duration
}

func NewExecutionProfile(
	maxCalls int,
	requestTimeout time.Duration,
	rateInterval time.Duration,
	providerInflight int,
	overhead time.Duration,
	configuredCollectTimeout time.Duration,
) (ExecutionProfile, error) {
	profile := ExecutionProfile{
		maxUpstreamCalls: maxCalls, requestTimeout: requestTimeout, rateInterval: rateInterval,
		providerInflight: providerInflight, overhead: overhead, collectTimeout: configuredCollectTimeout,
	}
	if configuredCollectTimeout == 0 {
		minimum, err := profile.minimum()
		if err != nil {
			return ExecutionProfile{}, fmt.Errorf("minimum: %w", err)
		}

		profile.collectTimeout = minimum
	}

	if err := profile.Validate(); err != nil {
		return ExecutionProfile{}, fmt.Errorf("validate: %w", err)
	}

	return profile, nil
}

func (p ExecutionProfile) MinimumCollectTimeout() time.Duration {
	value, err := p.minimum()
	if err != nil {
		return 0
	}

	return value
}

func (p ExecutionProfile) CollectTimeout() time.Duration {
	return p.collectTimeout
}

func (p ExecutionProfile) Validate() error {
	minimum, err := p.minimum()
	if err != nil {
		return fmt.Errorf("minimum: %w", err)
	}

	if p.collectTimeout < minimum {
		return errors.New("validate execution profile: collect timeout is below minimum")
	}

	return nil
}

func (p ExecutionProfile) minimum() (time.Duration, error) {
	if p.valuesOutsideBounds() {
		return 0, errors.New("validate execution profile: values are outside bounds")
	}

	requestBudget, err := checkedMulDuration(p.maxUpstreamCalls, p.requestTimeout)
	if err != nil {
		return 0, fmt.Errorf("checked mul duration: %w", err)
	}

	if p.maxUpstreamCalls > math.MaxInt/p.providerInflight {
		return 0, errors.New("validate execution profile: reservation count overflows")
	}

	// 직전 작업의 마지막 예약부터 남은 간격도 첫 요청의 대기에 포함됩니다.
	reservationCount := p.maxUpstreamCalls * p.providerInflight

	limiterBudget, err := checkedMulDuration(reservationCount, p.rateInterval)
	if err != nil {
		return 0, fmt.Errorf("checked mul duration: %w", err)
	}

	out, err := checkedAddDuration(requestBudget, limiterBudget, p.overhead)
	if err != nil {
		return out, fmt.Errorf("checked add duration: %w", err)
	}

	return out, nil
}

func (p ExecutionProfile) valuesOutsideBounds() bool {
	return p.maxUpstreamCalls < 1 || p.providerInflight < 1 || p.requestTimeout <= 0 || p.rateInterval < 0 || p.overhead <= 0
}

func checkedMulDuration(n int, duration time.Duration) (time.Duration, error) {
	if n < 0 || duration < 0 || (duration > 0 && int64(n) > math.MaxInt64/int64(duration)) {
		return 0, errors.New("validate execution profile: duration multiplication overflows")
	}

	return time.Duration(n) * duration, nil
}

func checkedAddDuration(values ...time.Duration) (time.Duration, error) {
	var result time.Duration

	for _, value := range values {
		if value < 0 || result > time.Duration(math.MaxInt64)-value {
			return 0, errors.New("validate execution profile: duration addition overflows")
		}

		result += value
	}

	return result, nil
}

type RegisteredRunner struct {
	runner   collection.JobRunner
	contract collection.JobContract
	profile  ExecutionProfile
}

func newRegisteredRunner(
	runner collection.JobRunner,
	job collection.JobContract,
	profile ExecutionProfile,
) (RegisteredRunner, error) {
	if runner == nil || runner.JobID() != job.ID() || job.Validate() != nil || profile.Validate() != nil {
		return RegisteredRunner{}, errors.New("register collection job runner: registration is invalid")
	}

	return RegisteredRunner{runner: runner, contract: job, profile: profile}, nil
}

func (r RegisteredRunner) Runner() collection.JobRunner     { return r.runner }
func (r RegisteredRunner) Contract() collection.JobContract { return r.contract }
func (r RegisteredRunner) Profile() ExecutionProfile        { return r.profile }

type runnerKey struct {
	provider contract.Provider
	jobKind  string
}

type Registry struct {
	runners []RegisteredRunner
	byKey   map[runnerKey]RegisteredRunner
}

func NewRegistryWithProfiles(profiles map[collection.JobID]ExecutionProfile, runners ...collection.JobRunner) (*Registry, error) {
	contracts := collection.InitialJobContracts()
	registry := &Registry{
		runners: make([]RegisteredRunner, 0, len(runners)),
		byKey:   make(map[runnerKey]RegisteredRunner, len(runners)),
	}
	seenContracts := make(map[collection.JobID]struct{}, len(contracts))

	for _, runner := range runners {
		if err := registerRunner(registry, contracts, seenContracts, profiles, runner); err != nil {
			return nil, fmt.Errorf("register runner: %w", err)
		}
	}

	if len(seenContracts) != len(contracts) {
		return nil, errors.New("register collection job runner: InitialJobContracts coverage is incomplete")
	}

	return registry, nil
}

func registerRunner(
	registry *Registry,
	contracts collection.JobContractSet,
	seenContracts map[collection.JobID]struct{},
	profiles map[collection.JobID]ExecutionProfile,
	runner collection.JobRunner,
) error {
	if runner == nil {
		return errors.New("register collection job runner: runner is nil")
	}

	id := runner.JobID()
	key := runnerKey{provider: id.Provider, jobKind: string(id.Kind)}

	if !key.provider.Valid() || key.jobKind == "" {
		return errors.New("register collection job runner: identity is invalid")
	}

	if _, exists := registry.byKey[key]; exists {
		return fmt.Errorf("register collection job runner: duplicate %s/%s", key.provider, key.jobKind)
	}

	definition, ok := contracts.Definition(id)
	if !ok {
		return fmt.Errorf("register collection job runner: unknown job kind %s", key.jobKind)
	}

	profile, ok := profiles[id]
	if !ok {
		return fmt.Errorf("register collection job runner: execution profile is missing for %s", id)
	}

	registration, err := newRegisteredRunner(runner, definition, profile)
	if err != nil {
		return fmt.Errorf("registered runner: %w", err)
	}

	registry.byKey[key] = registration
	registry.runners = append(registry.runners, registration)
	seenContracts[id] = struct{}{}

	return nil
}

func (r *Registry) Runners() []RegisteredRunner {
	if r == nil {
		return nil
	}

	return slices.Clone(r.runners)
}

func (r *Registry) Lookup(provider contract.Provider, jobKind string) (RegisteredRunner, bool) {
	if r == nil {
		return RegisteredRunner{}, false
	}

	runner, ok := r.byKey[runnerKey{provider: provider, jobKind: jobKind}]

	return runner, ok
}
