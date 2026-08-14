package stats

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceEqualConsecutiveSamplesAreBothRetainedByScheduledSlot(t *testing.T) {
	t.Parallel()
	first := statsAt(1, contract.ProviderYouTubeJS, t1(), 10, 20, 3)
	second := statsAt(2, contract.ProviderYouTubeJS, t2(), 10, 20, 3)
	got := mustReduceAll(t, State{}, []Evidence{first, second})
	if got.Head.LastResolvedScheduledFor == nil || !got.Head.LastResolvedScheduledFor.Equal(t2()) {
		t.Fatalf("last resolved = %v, want t2", got.Head.LastResolvedScheduledFor)
	}
	if !got.WriteSnapshot || got.Sample == nil || !got.Sample.ScheduledFor.Equal(t2()) {
		t.Fatalf("second equal sample not retained: %#v", got)
	}
}

func TestReduceHiddenCountRemainsNil(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, State{}, []Evidence{{
		ObservationID: 1,
		Provider:      contract.ProviderYouTubeJS,
		Sample: Sample{
			ChannelID: "UC_TEST", SubscriberCovered: true, ViewCovered: true, VideoCovered: true,
			ScheduledFor: t1(), EffectiveAt: t1(), ReceivedAt: t1(),
		},
	}})
	if got.Sample == nil || got.Sample.SubscriberCount != nil || got.Sample.ViewCount != nil || got.Sample.VideoCount != nil {
		t.Fatalf("hidden counts must stay nil: %#v", got.Sample)
	}
	if got.Head.LastResolvedSubscriberCount != nil || got.Head.LastResolvedViewCount != nil || got.Head.LastResolvedVideoCount != nil {
		t.Fatalf("hidden canonical must stay nil: %#v", got.Head)
	}
}

func TestReduceEqualTimeConflictDoesNotArrivalOrderOverwrite(t *testing.T) {
	t.Parallel()
	first := statsAt(1, contract.ProviderYouTubeJS, t1(), 10, 20, 3)
	second := statsAt(2, contract.ProviderHolodex, t1(), 99, 20, 3)
	forward := mustReduceAll(t, State{}, []Evidence{first, second})
	reverse := mustReduceAll(t, State{}, []Evidence{second, first})
	if forward.Conflict == nil || reverse.Conflict == nil {
		t.Fatal("equal-time conflict must be recorded")
	}
	if !forward.ClearSnapshot || !reverse.ClearSnapshot {
		t.Fatal("conflicting slot must not stay as canonical snapshot")
	}
	if forward.Head.LastResolvedScheduledFor != nil || reverse.Head.LastResolvedScheduledFor != nil {
		t.Fatalf("conflict must not leave a resolved slot: %#v %#v", forward.Head, reverse.Head)
	}
}

func TestReduceComplementaryCoverageDoesNotConflict(t *testing.T) {
	t.Parallel()
	full := statsAt(1, contract.ProviderYouTubeJS, t1(), 10, 20, 3)
	partial := Evidence{
		ObservationID: 2,
		Provider:      contract.ProviderHolodex,
		Sample: Sample{
			ChannelID: "UC_TEST", Provider: contract.ProviderHolodex,
			SubscriberCount: int64Ptr(10), VideoCount: int64Ptr(3),
			SubscriberCovered: true, VideoCovered: true,
			ObservationID: 2, ScheduledFor: t1(), EffectiveAt: t1(), ReceivedAt: t1(),
		},
	}
	got := mustReduceAll(t, State{}, []Evidence{full, partial})
	if got.Conflict != nil || got.ClearSnapshot {
		t.Fatalf("overlapping equal fields must not conflict: %#v", got)
	}
}

func TestReduceProviderArrivalPermutationsYieldSameProjection(t *testing.T) {
	t.Parallel()
	a1 := statsAt(1, contract.ProviderYouTubeJS, t1(), 10, 20, 3)
	b1 := statsAt(2, contract.ProviderHolodex, t1(), 10, 20, 3)
	a2 := statsAt(3, contract.ProviderYouTubeJS, t2(), 11, 21, 4)
	forward := mustReduceAll(t, State{}, []Evidence{a1, b1, a2})
	reverse := mustReduceAll(t, State{}, []Evidence{b1, a1, a2})
	if !sameHead(forward.Head, reverse.Head) {
		t.Fatalf("permutation heads differ: %#v vs %#v", forward.Head, reverse.Head)
	}
	if forward.Head.LastResolvedScheduledFor == nil || !forward.Head.LastResolvedScheduledFor.Equal(t2()) {
		t.Fatalf("last resolved = %v", forward.Head.LastResolvedScheduledFor)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func t1() time.Time { return time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC) }
func t2() time.Time { return time.Date(2026, 8, 14, 7, 0, 0, 0, time.UTC) }

func statsAt(id int64, provider contract.Provider, at time.Time, sub, views, videos int64) Evidence {
	return Evidence{
		ObservationID: id,
		Provider:      provider,
		Sample: Sample{
			ChannelID: "UC_TEST", Provider: provider,
			SubscriberCount: &sub, ViewCount: &views, VideoCount: &videos,
			SubscriberCovered: true, ViewCovered: true, VideoCovered: true,
			ObservationID: id, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		},
	}
}

func mustReduceAll(t *testing.T, state State, evidence []Evidence) Decision {
	t.Helper()
	current := state
	var decision Decision
	var slot time.Time
	for i := range evidence {
		if !slot.IsZero() && !evidence[i].Sample.ScheduledFor.Equal(slot) {
			current.Slot = nil
		}
		slot = evidence[i].Sample.ScheduledFor
		next, err := Reduce(current, evidence[i])
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}
		decision = next
		current = stateFromDecision(current, next, evidence[i])
	}
	return decision
}

func stateFromDecision(previous State, decision Decision, evidence Evidence) State {
	next := previous
	next.ChannelID = evidence.Sample.ChannelID
	next.Head = decision.Head
	if decision.Sample == nil {
		return next
	}
	digest, err := sampleDigest(*decision.Sample)
	if err != nil {
		return next
	}
	item := SlotEvidence{
		Provider: evidence.Provider, SubscriberCount: decision.Sample.SubscriberCount,
		ViewCount: decision.Sample.ViewCount, VideoCount: decision.Sample.VideoCount,
		SubscriberCovered: decision.Sample.SubscriberCovered, ViewCovered: decision.Sample.ViewCovered,
		VideoCovered: decision.Sample.VideoCovered, Digest: digest,
	}
	replaced := false
	for i := range next.Slot {
		if next.Slot[i].Provider == evidence.Provider {
			next.Slot[i] = item
			replaced = true
		}
	}
	if !replaced {
		next.Slot = append(next.Slot, item)
	}
	return next
}

func sameHead(left, right Head) bool {
	return sameOptionalTime(left.LastResolvedScheduledFor, right.LastResolvedScheduledFor) &&
		sameOptionalCount(left.LastResolvedSubscriberCount, right.LastResolvedSubscriberCount) &&
		sameOptionalCount(left.LastResolvedViewCount, right.LastResolvedViewCount) &&
		sameOptionalCount(left.LastResolvedVideoCount, right.LastResolvedVideoCount) &&
		sameOptionalTime(left.UnresolvedScheduledFor, right.UnresolvedScheduledFor)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalCount(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
