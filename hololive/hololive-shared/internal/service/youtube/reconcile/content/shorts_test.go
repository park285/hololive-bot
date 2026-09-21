package content

import (
	"fmt"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestShortsPartialWindowInitializesWithoutCompleteEvidence(t *testing.T) {
	state := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	baseline := shortsAt(1, "known")
	first := mustReduceAll(t, state, []Evidence{baseline}, 0)
	assertNotifications(t, first)
	if !first.Watermark.Initialized || first.EarliestCompleteAt != nil {
		t.Fatal("partial baseline must initialize notifications without claiming complete coverage")
	}

	got := mustReduceAll(t, state, []Evidence{baseline, shortsAt(2, "known", "new")}, 0)
	assertNotifications(t, got, "short:new")
}

func TestShortsPreviouslyStoredWithoutNotificationsStaySilent(t *testing.T) {
	ids := make([]string, 49)
	for i := range ids {
		ids[i] = fmt.Sprintf("old-%d", i)
	}
	baseline := shortsAt(1, ids...)
	state := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	first := mustReduceAll(t, state, []Evidence{baseline}, 0)
	stateAfterBaseline := stateFromDecision(state, first, &baseline)
	if stateAfterBaseline.EarliestCompleteAt != nil {
		t.Fatal("regression requires the production partial-only state")
	}

	got := mustReduceAll(t, &stateAfterBaseline, []Evidence{shortsAt(2, append(ids, "new")...)}, 0)
	assertNotifications(t, got, "short:new")
	got = mustReduceAll(t, &stateAfterBaseline, []Evidence{baseline, shortsAt(2, ids...)}, 0)
	assertNotifications(t, got)
}

func TestShortsEmptyPartialDoesNotInitialize(t *testing.T) {
	state := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	first := mustReduceAll(t, state, []Evidence{shortsAt(1)}, 0)
	if first.Watermark.Initialized {
		t.Fatal("empty partial evidence cannot establish a baseline")
	}
	got := mustReduceAll(t, state, []Evidence{shortsAt(1), shortsAt(2, "known")}, 0)
	assertNotifications(t, got)
}

func TestShortsCompleteEmptyBaselineAllowsFirstContent(t *testing.T) {
	state := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	baseline := shortsAt(1)
	baseline.Completeness = contract.CompletenessComplete
	baseline.Continuity = contract.ContinuityContiguous
	baseline.Coverage.Shorts.Exhausted = true
	got := mustReduceAll(t, state, []Evidence{baseline, shortsAt(2, "new")}, 0)
	assertNotifications(t, got, "short:new")
}

func TestShortsReorderMetadataAndReplayDoNotRearm(t *testing.T) {
	state := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	baseline := shortsAt(1, "known-a", "known-b")
	novel := shortsAt(2, "known-b", "new", "known-a")
	updated := shortsAt(3, "new", "known-a", "known-b")
	updated.Videos[0].Title = "Changed title"
	got := mustReduceAll(t, state, []Evidence{baseline, novel, updated, novel}, 0)
	assertNotifications(t, got)
	if got.EarliestCompleteAt != nil {
		t.Fatal("partial observations must not become complete")
	}
}

func TestShortsInitializedWindowNotificationSetConverges(t *testing.T) {
	initial := &State{ChannelID: testChannelID, Kind: contract.KindShortsList}
	baseline := shortsAt(1, "known")
	first := mustReduceAll(t, initial, []Evidence{baseline}, 0)
	seeded := stateFromDecision(initial, first, &baseline)
	evidence := []Evidence{shortsAt(2, "new-a", "known"), shortsAt(3, "new-b", "new-a"), shortsAt(4, "known", "new-b")}
	for _, order := range permutations(evidence) {
		state := seeded.clone()
		var notifications []string
		for i := range order {
			decision := mustReduceAll(t, &state, []Evidence{order[i]}, 0)
			for _, intent := range decision.Notifications {
				notifications = append(notifications, intent.ContentID)
			}
			state = stateFromDecision(&state, decision, &order[i])
		}
		slices.Sort(notifications)
		if !slices.Equal(notifications, []string{"short:new-a", "short:new-b"}) {
			t.Fatalf("order %v emitted %v", observationIDs(order), notifications)
		}
	}
}

func shortsAt(id int64, ids ...string) Evidence {
	at := time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC).Add(time.Duration(id) * time.Minute)
	evidence := positiveAt(id, at, Entity{})
	evidence.Kind = contract.KindShortsList
	evidence.Completeness = contract.CompletenessPartial
	evidence.Continuity = contract.ContinuityGapUnresolved
	evidence.Coverage = ShortsCoverage(&contract.ShortsListCoverageV1{ChannelID: testChannelID, MaxResults: 100})
	evidence.Videos = make([]Entity, 0, len(ids))
	for _, videoID := range ids {
		evidence.Videos = append(evidence.Videos, Entity{
			VideoID: videoID, ChannelID: testChannelID, Title: videoID, IsShort: true,
		})
	}
	return evidence
}
