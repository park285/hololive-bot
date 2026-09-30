package live

import (
	"testing"
	"time"
)

func TestUnconfirmedFactsDoNotRefreshRetainedLiveStatusClock(t *testing.T) {
	for _, upcoming := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed LIVE", true: "UPCOMING keeps LIVE"}[upcoming], func(t *testing.T) {
			positive := liveA()
			state := emptyState()

			state.Sessions[testVideoID] = sessionOf(mustReduceAll(t, emptyState(), []Evidence{positive}, 0))

			incoming := liveA()
			incoming.ObservationID++

			incoming.EffectiveAt = positive.EffectiveAt.Add(time.Minute)
			incoming.ReceivedAt = incoming.EffectiveAt
			incoming.ScheduledFor = incoming.EffectiveAt
			incoming.Sessions[0].LiveStartConfirmed = false
			incoming.Sessions[0].Title = "new metadata"

			if upcoming {
				incoming.Sessions[0].Status = string(StatusUpcoming)
			}

			decision, err := Reduce(*state, incoming, 0, incoming.ReceivedAt)
			if err != nil {
				t.Fatal(err)
			}

			if len(decision.Sessions) != 1 {
				t.Fatalf("metadata decision = %#v", decision)
			}

			got := decision.Sessions[0]
			if got.Status != StatusLive || got.StatusObservedAt == nil || !got.StatusObservedAt.Equal(positive.EffectiveAt) {
				t.Fatalf("metadata refreshed retained LIVE provenance: %#v", got)
			}
		})
	}
}
