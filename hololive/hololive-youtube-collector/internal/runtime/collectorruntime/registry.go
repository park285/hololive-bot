package collectorruntime

import (
	"fmt"
	"math"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type JobRunner = collectutil.JobRunner
type RunInput = collectutil.RunInput
type RunOutput = collectutil.RunOutput
type CollectResult = collectutil.CollectResult

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
			return ExecutionProfile{}, err
		}
		profile.collectTimeout = minimum
	}
	if err := profile.Validate(); err != nil {
		return ExecutionProfile{}, err
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
		return err
	}
	if p.collectTimeout < minimum {
		return fmt.Errorf("validate execution profile: collect timeout is below minimum")
	}
	return nil
}

func (p ExecutionProfile) minimum() (time.Duration, error) {
	if p.maxUpstreamCalls < 1 || p.providerInflight < 1 || p.requestTimeout <= 0 || p.rateInterval < 0 || p.overhead <= 0 {
		return 0, fmt.Errorf("validate execution profile: values are outside bounds")
	}
	requestBudget, err := checkedMulDuration(p.maxUpstreamCalls, p.requestTimeout)
	if err != nil {
		return 0, err
	}
	if p.maxUpstreamCalls > math.MaxInt/p.providerInflight {
		return 0, fmt.Errorf("validate execution profile: reservation count overflows")
	}
	reservationCount := p.maxUpstreamCalls*p.providerInflight - 1
	limiterBudget, err := checkedMulDuration(reservationCount, p.rateInterval)
	if err != nil {
		return 0, err
	}
	return checkedAddDuration(requestBudget, limiterBudget, p.overhead)
}

func checkedMulDuration(n int, duration time.Duration) (time.Duration, error) {
	if n < 0 || duration < 0 || (duration > 0 && int64(n) > math.MaxInt64/int64(duration)) {
		return 0, fmt.Errorf("validate execution profile: duration multiplication overflows")
	}
	return time.Duration(n) * duration, nil
}

func checkedAddDuration(values ...time.Duration) (time.Duration, error) {
	var result time.Duration
	for _, value := range values {
		if value < 0 || result > time.Duration(math.MaxInt64)-value {
			return 0, fmt.Errorf("validate execution profile: duration addition overflows")
		}
		result += value
	}
	return result, nil
}

type RegisteredRunner struct {
	runner   JobRunner
	contract sourceobservation.JobContract
	profile  ExecutionProfile
}

func newRegisteredRunner(
	runner JobRunner,
	job sourceobservation.JobContract,
	profile ExecutionProfile,
) (RegisteredRunner, error) {
	if runner == nil || runner.JobID() != job.ID() || job.Validate() != nil || profile.Validate() != nil {
		return RegisteredRunner{}, fmt.Errorf("register collection job runner: registration is invalid")
	}
	return RegisteredRunner{runner: runner, contract: job.Clone(), profile: profile}, nil
}

func (r RegisteredRunner) Runner() JobRunner                       { return r.runner }
func (r RegisteredRunner) Contract() sourceobservation.JobContract { return r.contract.Clone() }
func (r RegisteredRunner) Profile() ExecutionProfile               { return r.profile }

type runnerKey struct {
	provider contract.Provider
	jobKind  string
}

type Registry struct {
	runners []RegisteredRunner
	byKey   map[runnerKey]RegisteredRunner
}

func NewRegistry(runners ...JobRunner) (*Registry, error) {
	profiles := make(map[sourceobservation.JobID]ExecutionProfile, len(runners))
	for _, runner := range runners {
		if runner == nil {
			continue
		}
		maxCalls := 1
		if string(runner.JobID().Kind) == "youtubejs_content" {
			maxCalls = 2
		}
		profile, err := NewExecutionProfile(maxCalls, time.Second, 0, 1, time.Second, 0)
		if err != nil {
			return nil, err
		}
		profiles[runner.JobID()] = profile
	}
	return NewRegistryWithProfiles(profiles, runners...)
}

func NewRegistryWithProfiles(profiles map[sourceobservation.JobID]ExecutionProfile, runners ...JobRunner) (*Registry, error) {
	contracts := sourceobservation.InitialJobContracts()
	registry := &Registry{
		runners: make([]RegisteredRunner, 0, len(runners)),
		byKey:   make(map[runnerKey]RegisteredRunner, len(runners)),
	}
	seenContracts := make(map[sourceobservation.JobID]struct{}, len(contracts))
	for _, runner := range runners {
		if err := registerRunner(registry, contracts, seenContracts, profiles, runner); err != nil {
			return nil, err
		}
	}
	if len(seenContracts) != len(contracts) {
		return nil, fmt.Errorf("register collection job runner: InitialJobContracts coverage is incomplete")
	}
	return registry, nil
}

func registerRunner(
	registry *Registry,
	contracts sourceobservation.JobContractSet,
	seenContracts map[sourceobservation.JobID]struct{},
	profiles map[sourceobservation.JobID]ExecutionProfile,
	runner JobRunner,
) error {
	if runner == nil {
		return fmt.Errorf("register collection job runner: runner is nil")
	}
	id := runner.JobID()
	key := runnerKey{provider: id.Provider, jobKind: string(id.Kind)}
	if !key.provider.Valid() || key.jobKind == "" {
		return fmt.Errorf("register collection job runner: identity is invalid")
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
		return err
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
