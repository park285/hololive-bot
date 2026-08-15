package collectorruntime

import (
	"fmt"
	"sort"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type JobRunner = collectutil.JobRunner
type RunInput = collectutil.RunInput
type RunOutput = collectutil.RunOutput

type runnerKey struct {
	provider contract.Provider
	jobKind  string
}

type Registry struct {
	runners []JobRunner
	byKey   map[runnerKey]JobRunner
}

func NewRegistry(runners ...JobRunner) (*Registry, error) {
	contracts := sourceobservation.InitialJobContracts()
	registry := &Registry{
		runners: make([]JobRunner, 0, len(runners)),
		byKey:   make(map[runnerKey]JobRunner, len(runners)),
	}
	seenContracts := make(map[string]struct{}, len(contracts))
	for _, runner := range runners {
		if err := registerRunner(registry, contracts, seenContracts, runner); err != nil {
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
	seenContracts map[string]struct{},
	runner JobRunner,
) error {
	if runner == nil {
		return fmt.Errorf("register collection job runner: runner is nil")
	}
	key := runnerKey{provider: runner.Provider(), jobKind: runner.JobKind()}
	if !key.provider.Valid() || key.jobKind == "" {
		return fmt.Errorf("register collection job runner: identity is invalid")
	}
	if _, exists := registry.byKey[key]; exists {
		return fmt.Errorf("register collection job runner: duplicate %s/%s", key.provider, key.jobKind)
	}
	definition, ok := contracts.Definition(key.jobKind)
	if !ok {
		return fmt.Errorf("register collection job runner: unknown job kind %s", key.jobKind)
	}
	if err := matchEmissions(key, definition, runner.Emissions()); err != nil {
		return err
	}
	if err := validateTargetKinds(key, runner.Emissions(), runner.TargetKinds()); err != nil {
		return err
	}
	registry.byKey[key] = runner
	registry.runners = append(registry.runners, runner)
	seenContracts[key.jobKind] = struct{}{}
	return nil
}

func validateTargetKinds(
	key runnerKey,
	emissions []contract.ObservationKind,
	targetKinds []contract.ObservationKind,
) error {
	if len(targetKinds) == 0 {
		return fmt.Errorf("register collection job runner: %s/%s has no target kinds", key.provider, key.jobKind)
	}
	targetSet, err := targetKindSet(key, targetKinds)
	if err != nil {
		return err
	}
	for _, kind := range emissions {
		if _, ok := targetSet[kind]; !ok {
			return fmt.Errorf("register collection job runner: %s/%s emission is missing from target kinds", key.provider, key.jobKind)
		}
	}
	return nil
}

func targetKindSet(key runnerKey, targetKinds []contract.ObservationKind) (map[contract.ObservationKind]struct{}, error) {
	targetSet := make(map[contract.ObservationKind]struct{}, len(targetKinds))
	for _, kind := range targetKinds {
		if !kind.Valid() {
			return nil, fmt.Errorf("register collection job runner: %s/%s target kind is invalid", key.provider, key.jobKind)
		}
		if _, exists := targetSet[kind]; exists {
			return nil, fmt.Errorf("register collection job runner: %s/%s target kind is duplicated", key.provider, key.jobKind)
		}
		targetSet[kind] = struct{}{}
	}
	return targetSet, nil
}

func matchEmissions(key runnerKey, definition sourceobservation.JobContract, emissions []contract.ObservationKind) error {
	expected := make([]contract.ObservationKind, 0, len(definition.Emissions))
	for _, emission := range definition.Emissions {
		if emission.Provider == key.provider {
			expected = append(expected, emission.Kind)
		}
	}
	if len(expected) == 0 {
		return fmt.Errorf("register collection job runner: %s/%s has no compile-time emissions", key.provider, key.jobKind)
	}
	if len(emissions) != len(expected) {
		return fmt.Errorf("register collection job runner: %s/%s emission count mismatch", key.provider, key.jobKind)
	}
	sortKinds(expected)
	actual := append([]contract.ObservationKind(nil), emissions...)
	sortKinds(actual)
	for i := range expected {
		if actual[i] != expected[i] {
			return fmt.Errorf("register collection job runner: %s/%s emission mismatch", key.provider, key.jobKind)
		}
	}
	return nil
}

func sortKinds(kinds []contract.ObservationKind) {
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
}

func (r *Registry) Runners() []JobRunner {
	if r == nil {
		return nil
	}
	return r.runners
}

func (r *Registry) Lookup(provider contract.Provider, jobKind string) (JobRunner, bool) {
	if r == nil {
		return nil, false
	}
	runner, ok := r.byKey[runnerKey{provider: provider, jobKind: jobKind}]
	return runner, ok
}
