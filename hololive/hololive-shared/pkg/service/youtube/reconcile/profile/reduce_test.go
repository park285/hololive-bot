package profile

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()
	effective := t1()
	originalEffective := effective
	state := State{ChannelID: "UC_TEST", Head: Head{ChannelID: "UC_TEST", Description: CanonicalField{
		Set: true, Value: "hello", EffectiveAt: &effective,
	}}}
	evidence := profileAt(2, t2(), Field{}, Field{})
	decision, err := Reduce(state, evidence, Policy{})
	if err != nil {
		t.Fatal(err)
	}

	*decision.Head.Description.EffectiveAt = effective.Add(time.Hour)
	decision.Head.Description.Value = "mutated decision"
	if !state.Head.Description.EffectiveAt.Equal(originalEffective) || state.Head.Description.Value != "hello" {
		t.Fatal("decision shares state profile storage")
	}
	if evidence.Sample.Description.Value != "" {
		t.Fatal("reducer mutated evidence")
	}
}

func TestReduceAbsentFieldDoesNotClear(t *testing.T) {
	t.Parallel()
	first := profileAt(1, t1(), Field{Present: true, Value: "hello"}, Field{Present: true, Value: "2019-01-02"})
	second := profileAt(2, t2(), Field{}, Field{})
	got := mustReduceAll(t, []Evidence{first, second}, Policy{})
	if !got.Head.Description.Set || got.Head.Description.Value != "hello" {
		t.Fatalf("absent field cleared description: %#v", got.Head.Description)
	}
}

func TestReduceExplicitEmptyRequiresStability(t *testing.T) {
	t.Parallel()
	seed := profileAt(1, t1(), Field{Present: true, Value: "hello"}, Field{})
	empty1 := profileAt(2, t2(), Field{Present: true, Value: ""}, Field{})
	disabled := mustReduceAll(t, []Evidence{seed, empty1}, Policy{})
	if !disabled.Head.Description.Set || disabled.Head.Description.Value != "hello" {
		t.Fatalf("disabled clear must keep description: %#v", disabled.Head.Description)
	}
	enabled := mustReduceAll(t, []Evidence{seed, empty1}, Policy{ClearMinObservations: 2, ClearStability: time.Hour})
	if enabled.Head.Description.Value != "hello" {
		t.Fatalf("single empty must not clear: %#v", enabled.Head.Description)
	}
	empty2 := profileAt(3, t2().Add(2*time.Hour), Field{Present: true, Value: ""}, Field{})
	cleared := mustReduceAll(t, []Evidence{seed, empty1, empty2}, Policy{ClearMinObservations: 2, ClearStability: time.Hour})
	if !cleared.Head.Description.Set || cleared.Head.Description.Value != "" {
		t.Fatalf("stable empty must clear: %#v", cleared.Head.Description)
	}
}

func TestReduceJoinedDateConflictIsRejectedAndRecorded(t *testing.T) {
	t.Parallel()
	first := profileAt(1, t1(), Field{}, Field{Present: true, Value: "2019-01-02"})
	second := profileAt(2, t2(), Field{}, Field{Present: true, Value: "2020-03-04"})
	got := mustReduceAll(t, []Evidence{first, second}, Policy{})
	if len(got.Conflicts) != 1 || got.Conflicts[0].FieldName != "joined_date" {
		t.Fatalf("joined date conflict = %#v", got.Conflicts)
	}
	if got.Head.JoinedDate.Value != "2019-01-02" {
		t.Fatalf("joined date overwritten: %#v", got.Head.JoinedDate)
	}
}

func TestReduceProfilePermutationsYieldSameProjection(t *testing.T) {
	t.Parallel()
	a := Evidence{
		ObservationID: 1, Provider: contract.ProviderYouTubeJS,
		Sample: Sample{
			ChannelID: "UC_TEST", Handle: Field{Present: true, Value: "@one"},
			Complete: true, ScheduledFor: t1(), EffectiveAt: t1(), ReceivedAt: t1(),
		},
	}
	b := Evidence{
		ObservationID: 2, Provider: contract.ProviderHolodex,
		Sample: Sample{
			ChannelID: "UC_TEST", Country: Field{Present: true, Value: "jp"},
			Complete: true, ScheduledFor: t2(), EffectiveAt: t2(), ReceivedAt: t2(),
		},
	}
	forward := mustReduceAll(t, []Evidence{a, b}, Policy{})
	reverse := mustReduceAll(t, []Evidence{b, a}, Policy{})
	if forward.Head.Handle.Value != reverse.Head.Handle.Value || forward.Head.Country.Value != reverse.Head.Country.Value {
		t.Fatalf("permutation projection differs: %#v vs %#v", forward.Head, reverse.Head)
	}
}

func t1() time.Time { return time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC) }
func t2() time.Time { return time.Date(2026, 8, 14, 7, 0, 0, 0, time.UTC) }

func profileAt(id int64, at time.Time, description, joined Field) Evidence {
	return Evidence{
		ObservationID: id,
		Provider:      contract.ProviderYouTubeJS,
		Sample: Sample{
			ChannelID: "UC_TEST", Description: description, JoinedDate: joined,
			Complete: true, ObservationID: id, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		},
	}
}

func mustReduceAll(t *testing.T, evidence []Evidence, policy Policy) Decision {
	t.Helper()
	current := State{}
	var decision Decision
	for i := range evidence {
		next, err := Reduce(current, evidence[i], policy)
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}
		decision = next
		current.Head = next.Head
		current.ChannelID = evidence[i].Sample.ChannelID
	}
	return decision
}
