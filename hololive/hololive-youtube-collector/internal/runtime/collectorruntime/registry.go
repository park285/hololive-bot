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
		if runner == nil {
			return nil, fmt.Errorf("register collection job runner: runner is nil")
		}
		key := runnerKey{provider: runner.Provider(), jobKind: runner.JobKind()}
		if !key.provider.Valid() || key.jobKind == "" {
			return nil, fmt.Errorf("register collection job runner: identity is invalid")
		}
		if _, exists := registry.byKey[key]; exists {
			return nil, fmt.Errorf("register collection job runner: duplicate %s/%s", key.provider, key.jobKind)
		}
		definition, ok := contracts.Definition(key.jobKind)
		if !ok {
			return nil, fmt.Errorf("register collection job runner: unknown job kind %s", key.jobKind)
		}
		if err := matchEmissions(key, definition, runner.Emissions()); err != nil {
			return nil, err
		}
		registry.byKey[key] = runner
		registry.runners = append(registry.runners, runner)
		seenContracts[key.jobKind] = struct{}{}
	}
	if len(seenContracts) != len(contracts) {
		return nil, fmt.Errorf("register collection job runner: InitialJobContracts coverage is incomplete")
	}
	return registry, nil
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
