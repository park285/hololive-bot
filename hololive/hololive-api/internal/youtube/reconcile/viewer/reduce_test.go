package viewer

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()

	lastWindow := t1()
	lastCount := int64(10)
	state := State{VideoID: "vid-a", Head: Head{
		VideoID: "vid-a", LastResolvedWindowStart: &lastWindow, LastResolvedCount: &lastCount,
	}}
	evidence := sampleAt(2, contract.ProviderHolodex, t2(), 20)

	decision, err := Reduce(state, evidence)
	if err != nil {
		t.Fatal(err)
	}

	*decision.Head.PriorResolvedCount = 99
	*decision.Sample.ViewerCount = 88

	if *state.Head.LastResolvedCount != 10 {
		t.Fatal("decision shares state count pointer")
	}

	if *evidence.Sample.ViewerCount != 20 {
		t.Fatal("decision shares evidence count pointer")
	}
}

func TestReduceEqualConsecutiveSamplesAreRetained(t *testing.T) {
	t.Parallel()

	first := sampleAt(1, contract.ProviderHolodex, t1(), 10)
	second := sampleAt(2, contract.ProviderHolodex, t2(), 10)
	got := mustReduceAll(t, []Evidence{first, second})

	if got.Head.LastResolvedWindowStart == nil || !got.Head.LastResolvedWindowStart.Equal(t2()) {
		t.Fatalf("last resolved window = %v, want t2", got.Head.LastResolvedWindowStart)
	}

	if got.Sample == nil || !got.Sample.WindowStart.Equal(t2()) || got.Sample.ViewerCount == nil || *got.Sample.ViewerCount != 10 {
		t.Fatalf("second sample not retained: %#v", got.Sample)
	}
}

func TestReduceEqualWindowConflictStaysUnresolved(t *testing.T) {
	t.Parallel()

	first := sampleAt(1, contract.ProviderHolodex, t1(), 10)
	second := sampleAt(2, contract.ProviderYouTubeJS, t2(), 20)
	third := sampleAt(3, contract.ProviderHolodex, t2(), 30)
	got := mustReduceAll(t, []Evidence{first, second, third})

	if got.Conflict == nil {
		t.Fatal("equal-window conflict must be recorded")
	}

	if got.Head.UnresolvedWindowStart == nil || !got.Head.UnresolvedWindowStart.Equal(t2()) {
		t.Fatalf("unresolved window = %v", got.Head.UnresolvedWindowStart)
	}

	if got.Head.LastResolvedWindowStart == nil || !got.Head.LastResolvedWindowStart.Equal(t1()) {
		t.Fatalf("last resolved = %v, want t1", got.Head.LastResolvedWindowStart)
	}

	if !got.ClearProduct {
		t.Fatal("conflicting window must not stay as last resolved product")
	}
}

func TestReduceHiddenRemainsNil(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, []Evidence{{
		ObservationID: 1,
		Provider:      contract.ProviderHolodex,
		Sample: Sample{
			VideoID: "vid-a", Availability: AvailabilityHidden, WindowStart: t1(), WindowSeconds: 120,
			ScheduledFor: t1(), EffectiveAt: t1(), ReceivedAt: t1(),
		},
	}})
	if got.Sample == nil || got.Sample.ViewerCount != nil {
		t.Fatalf("hidden sample must keep a nil count: %#v", got.Sample)
	}

	if got.Head.LastResolvedAvailability != AvailabilityHidden || got.Head.LastResolvedCount != nil {
		t.Fatalf("hidden canonical = %#v", got.Head)
	}
}

func t1() time.Time { return time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC) }
func t2() time.Time { return time.Date(2026, time.August, 14, 1, 2, 0, 0, time.UTC) }

func sampleAt(id int64, provider contract.Provider, at time.Time, count int64) Evidence {
	value := count

	return Evidence{
		ObservationID: id,
		Provider:      provider,
		Sample: Sample{
			VideoID: "vid-a", ViewerCount: &value, Availability: AvailabilityAvailable,
			WindowStart: at, WindowSeconds: 120, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		},
	}
}

func mustReduceAll(t *testing.T, evidence []Evidence) *Decision {
	t.Helper()

	current := State{}

	var decision Decision

	for i := range evidence {
		next, err := Reduce(current, evidence[i])
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}

		decision = next
		current = stateFromDecision(&current, &next, &evidence[i])
	}

	return &decision
}

func stateFromDecision(previous *State, decision *Decision, evidence *Evidence) State {
	next := *previous

	next.VideoID = evidence.Sample.VideoID
	next.Head = decision.Head

	if decision.Sample == nil {
		return next
	}

	digest, err := sampleDigest(decision.Sample.Availability, decision.Sample.ViewerCount)
	if err != nil {
		return next
	}

	replaced := false

	for i := range next.Window {
		if next.Window[i].Provider == evidence.Provider {
			next.Window[i] = WindowEvidence{
				Provider: evidence.Provider, ViewerCount: decision.Sample.ViewerCount,
				Availability: decision.Sample.Availability, Digest: digest,
			}
			replaced = true
		}
	}

	if !replaced {
		next.Window = append(next.Window, WindowEvidence{
			Provider: evidence.Provider, ViewerCount: decision.Sample.ViewerCount,
			Availability: decision.Sample.Availability, Digest: digest,
		})
	}

	if decision.Head.UnresolvedWindowStart != nil && !decision.Head.UnresolvedWindowStart.Equal(evidence.Sample.WindowStart) {
		next.Window = nil
	}

	if decision.Head.LastResolvedWindowStart != nil && evidence.Sample.WindowStart.After(*decision.Head.LastResolvedWindowStart) {
		next.Window = []WindowEvidence{{
			Provider: evidence.Provider, ViewerCount: decision.Sample.ViewerCount,
			Availability: decision.Sample.Availability, Digest: digest,
		}}
	}

	return next
}
