package schedule

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()
	stateTime := at().Add(-time.Hour)
	originalStateTime := stateTime
	state := State{Sessions: map[string]Session{
		"vid-a": {VideoID: "vid-a", ChannelID: "UC_TEST", Status: domain.LiveStatusUpcoming, ScheduledStartTime: &stateTime},
	}}
	endedAt := at().Add(time.Hour)
	originalEndedAt := endedAt
	incoming := evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", ScheduledAt: at(), EndedAt: &endedAt,
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
	if !state.Sessions["vid-a"].ScheduledStartTime.Equal(originalStateTime) {
		t.Fatal("decision shares state session pointer")
	}
}

func TestReduceYouTubeVideoIDIsCanonicalIdentity(t *testing.T) {
	t.Parallel()
	got := mustReduce(t, State{}, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: "ext-1", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Official", ScheduledAt: at(), IsLive: true,
	}))
	if len(got.Sessions) != 1 || got.Sessions[0].VideoID != "vid-a" {
		t.Fatalf("canonical session = %#v", got.Sessions)
	}
	if got.Sessions[0].Status != domain.LiveStatusUpcoming {
		t.Fatalf("isLive must not create LIVE, status=%s", got.Sessions[0].Status)
	}
}

func TestReduceOfficialIsLiveDoesNotFlipLive(t *testing.T) {
	t.Parallel()
	state := State{Sessions: map[string]Session{
		"vid-a": {VideoID: "vid-a", ChannelID: "UC_TEST", Status: domain.LiveStatusUpcoming, Title: "Soon"},
	}}
	got := mustReduce(t, state, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Now", ScheduledAt: at(), IsLive: true,
	}))
	if len(got.Sessions) != 1 || got.Sessions[0].Status != domain.LiveStatusUpcoming {
		t.Fatalf("official isLive flipped LIVE: %#v", got.Sessions)
	}
}

func TestReduceTemporaryIdentityDoesNotMergeIntoYouTubeSession(t *testing.T) {
	t.Parallel()
	state := State{Sessions: map[string]Session{
		"vid-a": {VideoID: "vid-a", ChannelID: "UC_TEST", Status: domain.LiveStatusUpcoming, Title: "Keep"},
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
		"vid-a": {VideoID: "vid-a", ChannelID: "UC_TEST", Status: domain.LiveStatusEnded, Title: "Old"},
	}}
	got := mustReduce(t, state, evidence(contract.ProviderHololiveOfficial, Item{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Again", ScheduledAt: at(), IsLive: true,
	}))
	if len(got.Sessions) != 0 {
		t.Fatalf("ended session was merged: %#v", got.Sessions)
	}
}

func at() time.Time { return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC) }

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
