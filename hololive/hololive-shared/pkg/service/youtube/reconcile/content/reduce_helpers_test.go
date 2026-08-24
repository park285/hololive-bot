package content

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const testChannelID = "UC_TEST"

func seededState() *State {
	earliest := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	return &State{
		ChannelID:          testChannelID,
		Kind:               contract.KindVideoList,
		Initialized:        true,
		EarliestCompleteAt: &earliest,
		Videos:             map[string]EntityState{},
	}
}

func wideCoverage() CoverageValue {
	return VideoCoverage(&contract.ChannelListCoverageV1{
		ChannelID: testChannelID, MaxResults: 10, Exhausted: true,
	})
}

func videoA() Entity {
	published := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	return Entity{VideoID: "vid-a", ChannelID: testChannelID, Title: "Alpha", PublishedAt: &published}
}

func positiveAt(id int64, at time.Time, entity Entity) Evidence {
	return Evidence{
		Kind:           contract.KindVideoList,
		ObservationID:  id,
		ScheduledFor:   at,
		EffectiveAt:    at,
		ReceivedAt:     at,
		Completeness:   contract.CompletenessComplete,
		Continuity:     contract.ContinuityContiguous,
		Videos:         []Entity{entity},
		Coverage:       wideCoverage(),
		ScopeSHA256:    strings.Repeat("ab", 32),
		EvidenceSHA256: strings.Repeat("cd", 32),
	}
}

func emptyAt(id int64, at time.Time, completeness contract.Completeness, coverage CoverageValue) Evidence {
	return Evidence{
		Kind:           contract.KindVideoList,
		ObservationID:  id,
		ScheduledFor:   at,
		EffectiveAt:    at,
		ReceivedAt:     at,
		Completeness:   completeness,
		Continuity:     contract.ContinuityContiguous,
		Coverage:       coverage,
		ScopeSHA256:    strings.Repeat("ef", 32),
		EvidenceSHA256: strings.Repeat("11", 32),
	}
}

func positiveA() Evidence {
	return positiveAt(1, time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC), videoA())
}

func latePositiveA() Evidence {
	return positiveAt(4, time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC), videoA())
}

func midPositiveA() Evidence {
	return positiveAt(7, time.Date(2026, time.August, 14, 1, 30, 0, 0, time.UTC), videoA())
}

func completeNegativeB() Evidence {
	return emptyAt(2, time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC), contract.CompletenessComplete, wideCoverage())
}

func completeNegativeC(grace time.Duration) Evidence {
	at := time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
	evidence := emptyAt(3, at, contract.CompletenessComplete, wideCoverage())

	evidence.ReceivedAt = at.Add(grace)
	evidence.EvidenceSHA256 = strings.Repeat("22", 32)

	return evidence
}

func partialNegative() Evidence {
	coverage := VideoCoverage(&contract.ChannelListCoverageV1{
		ChannelID: testChannelID, MaxResults: 10, Exhausted: false,
	})

	return emptyAt(5, time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC), contract.CompletenessPartial, coverage)
}

func narrowNegative() Evidence {
	after := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	coverage := VideoCoverage(&contract.ChannelListCoverageV1{
		ChannelID: testChannelID, MaxResults: 10, Exhausted: true,
		Filters: contract.VideoListFiltersV1{PublishedAfter: &after},
	})

	return emptyAt(6, time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC), contract.CompletenessComplete, coverage)
}

func mustReduceAll(t *testing.T, state *State, evidence []Evidence, grace time.Duration) *Decision {
	t.Helper()

	current := state.clone()

	var decision Decision

	for i := range evidence {
		next, err := Reduce(current, evidence[i], grace)
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}

		decision = next
		current = stateFromDecision(&current, &next, &evidence[i])
	}

	return &decision
}

func stateFromDecision(previous *State, decision *Decision, evidence *Evidence) State {
	next := previous.clone()

	next.Initialized = true
	next.Kind = evidence.Kind

	if decision.Watermark != nil {
		next.ChannelID = decision.Watermark.ChannelID
		next.LastContentID = decision.Watermark.LastContentID
	}

	next.EarliestCompleteAt = decision.EarliestCompleteAt
	if next.Videos == nil {
		next.Videos = map[string]EntityState{}
	}

	for i := range decision.Clocks {
		clock := decision.Clocks[i]

		next.Videos[clock.VideoID] = clock
	}

	if decision.AbsenceSlot != nil {
		replaced := false

		for i := range next.AbsenceSlots {
			if next.AbsenceSlots[i].ScheduledFor.Equal(decision.AbsenceSlot.ScheduledFor) {
				next.AbsenceSlots[i] = *decision.AbsenceSlot
				replaced = true
			}
		}

		if !replaced {
			next.AbsenceSlots = append(next.AbsenceSlots, *decision.AbsenceSlot)
		}
	}

	return next
}

func snapshotDecision(decision *Decision) string {
	videos := make([]string, 0, len(decision.Clocks))
	for i := range decision.Clocks {
		clock := &decision.Clocks[i]

		videos = append(videos, fmt.Sprintf("%s|%s|missing=%t|withdrawn=%t|slots=%d",
			clock.VideoID, clock.Title,
			clock.Clock.MissingSinceEffectiveAt != nil,
			clock.WithdrawnAt != nil,
			clock.ConsecutiveAbsenceSlots,
		))
	}

	sort.Strings(videos)

	return strings.Join(videos, ";")
}

func assertAllPermutationsConverge(t *testing.T, state *State, evidence []Evidence, grace time.Duration) {
	t.Helper()

	var want string

	for _, order := range permutations(evidence) {
		got := snapshotDecision(mustReduceAll(t, state, order, grace))

		if want == "" {
			want = got
			continue
		}

		if got != want {
			t.Fatalf("permutation %v diverged\n got %s\nwant %s", observationIDs(order), got, want)
		}
	}
}

func permutations(items []Evidence) [][]Evidence {
	if len(items) == 0 {
		return [][]Evidence{{}}
	}

	var result [][]Evidence

	for i := range items {
		rest := append([]Evidence{}, items[:i]...)

		rest = append(rest, items[i+1:]...)

		for _, perm := range permutations(rest) {
			result = append(result, append([]Evidence{items[i]}, perm...))
		}
	}

	return result
}

func observationIDs(items []Evidence) []int64 {
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ObservationID
	}

	return ids
}

func clockOf(decision *Decision) EntityState {
	for i := range decision.Clocks {
		if decision.Clocks[i].VideoID == "vid-a" {
			return decision.Clocks[i]
		}
	}

	return EntityState{}
}

func assertVideoPresent(t *testing.T, decision *Decision) {
	t.Helper()

	if clockOf(decision).VideoID != "vid-a" {
		t.Fatal("missing canonical video vid-a")
	}
}

func assertMissing(t *testing.T, decision *Decision, want bool) {
	t.Helper()

	got := clockOf(decision).Clock.MissingSinceEffectiveAt != nil
	if got != want {
		t.Fatalf("video vid-a missing = %t, want %t", got, want)
	}
}

func assertWithdrawn(t *testing.T, decision *Decision, want bool) {
	t.Helper()

	got := clockOf(decision).WithdrawnAt != nil
	if got != want {
		t.Fatalf("video vid-a withdrawn = %t, want %t", got, want)
	}
}

func assertNotifications(t *testing.T, decision *Decision, videoIDs ...string) {
	t.Helper()

	got := make([]string, 0, len(decision.Notifications))
	for i := range decision.Notifications {
		got = append(got, decision.Notifications[i].ContentID)
	}

	sort.Strings(got)

	want := append([]string{}, videoIDs...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("notifications = %v, want %v", got, want)
	}
}
