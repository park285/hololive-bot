package schedule

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testVideoID   = "vid-a"
	testChannelID = "UC_TEST"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()

	stateTime := at().Add(-time.Hour)
	originalStateTime := stateTime
	state := State{Sessions: map[string]Session{
		testVideoID: {VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusUpcoming, ScheduledStartTime: &stateTime},
	}}
	endedAt := at().Add(time.Hour)
	originalEndedAt := endedAt
	incoming := evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, ScheduledAt: at(), EndedAt: &endedAt,
	})

	decision, err := Reduce(state, *incoming)
	if err != nil {
		t.Fatal(err)
	}

	*incoming.Items[0].EndedAt = endedAt.Add(time.Hour)
	if decision.Items[0].EndedAt == nil || !decision.Items[0].EndedAt.Equal(originalEndedAt) {
		t.Fatal("decision shares evidence item pointer")
	}

	*decision.Sessions[0].ScheduledStartTime = at().Add(2 * time.Hour)
	if !state.Sessions[testVideoID].ScheduledStartTime.Equal(originalStateTime) {
		t.Fatal("decision shares state session pointer")
	}
}

func TestReduceYouTubeVideoIDIsCanonicalIdentity(t *testing.T) {
	t.Parallel()

	got := mustReduce(t, State{}, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: "ext-1", VideoID: testVideoID, ChannelID: testChannelID, Title: "Official", ScheduledAt: at(), IsLive: true,
	}))
	if len(got.Sessions) != 1 || got.Sessions[0].VideoID != testVideoID {
		t.Fatalf("canonical session = %#v", got.Sessions)
	}

	if got.Sessions[0].Status != domain.LiveStatusUpcoming {
		t.Fatalf("isLive must not create LIVE, status=%s", got.Sessions[0].Status)
	}
}

func TestReducePreservesCollaboTalentNames(t *testing.T) {
	t.Parallel()

	incoming := evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: testVideoID, VideoID: testVideoID, Title: "Collab", ScheduledAt: at(),
		CollaboTalentNames: []string{"Guest One"},
	})
	got := mustReduce(t, State{}, incoming)

	incoming.Items[0].CollaboTalentNames[0] = "mutated"

	if len(got.Items) != 1 || got.Items[0].CollaboTalentNames[0] != "Guest One" {
		t.Fatalf("names = %#v", got.Items)
	}
}

func TestReduceOfficialIsLiveDoesNotFlipLive(t *testing.T) {
	t.Parallel()

	state := State{Sessions: map[string]Session{
		testVideoID: {VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusUpcoming, Title: "Soon"},
	}}
	got := mustReduce(t, state, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Now", ScheduledAt: at(), IsLive: true,
	}))

	if len(got.Sessions) != 1 || got.Sessions[0].Status != domain.LiveStatusUpcoming {
		t.Fatalf("official isLive flipped LIVE: %#v", got.Sessions)
	}
}

func TestReduceTemporaryIdentityDoesNotMergeIntoYouTubeSession(t *testing.T) {
	t.Parallel()

	state := State{Sessions: map[string]Session{
		testVideoID: {VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusUpcoming, Title: "Keep"},
	}}
	got := mustReduce(t, state, evidence(contract.ProviderHolodex, Item{
		ExternalID: "holodex-temp", Title: "Temp", ScheduledAt: at(),
	}))

	if len(got.Sessions) != 0 {
		t.Fatalf("temporary item merged into a session: %#v", got.Sessions)
	}

	if len(got.Items) != 1 || got.Items[0].VideoID != "" {
		t.Fatalf("temporary item = %#v", got.Items)
	}

	if ItemIdentity(contract.ProviderHolodex, &got.Items[0]) != "tmp:holodex:holodex-temp" {
		t.Fatalf("temporary identity = %s", ItemIdentity(contract.ProviderHolodex, &got.Items[0]))
	}
}

func TestReduceDoesNotReactivateEndedSession(t *testing.T) {
	t.Parallel()

	state := State{Sessions: map[string]Session{
		testVideoID: {VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusEnded, Title: "Old"},
	}}
	got := mustReduce(t, state, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Again", ScheduledAt: at(), IsLive: true,
	}))

	if len(got.Sessions) != 0 {
		t.Fatalf("ended session was merged: %#v", got.Sessions)
	}
}

func at() time.Time { return time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC) }

func evidence(provider contract.Provider, items ...Item) *Evidence {
	return &Evidence{
		ObservationID: 1,
		Provider:      provider,
		GroupKey:      "global:hololive-schedule",
		Items:         items,
		EffectiveAt:   at(),
		ReceivedAt:    at(),
	}
}

func mustReduce(t *testing.T, state State, evidence *Evidence) Decision {
	t.Helper()

	got, err := Reduce(state, *evidence)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}

	return got
}
