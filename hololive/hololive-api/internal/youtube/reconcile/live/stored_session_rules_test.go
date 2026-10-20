package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// storedSessionAt은 잠금 안에서 읽은 세션 행의 관측 시각이다. 저장 SQL은 reducer 결과를 그대로 쓰므로
// 저장된 세션 행을 지키는 규칙은 모두 reducer 결과에서 확인한다.
var storedSessionAt = time.Date(2026, time.August, 14, 1, 30, 0, 0, time.UTC)

// stateWithSession은 잠금 안에서 읽은 세션 하나를 가진 상태를 만든다.
func stateWithSession(session *SessionState) *State {
	state := emptyState()

	state.Sessions[session.VideoID] = *session

	return state
}

func TestReduceKeepsStoredMetadataOnSameTimeObservation(t *testing.T) {
	t.Parallel()

	scheduled := storedSessionAt.Add(time.Hour)
	state := stateWithSession(&SessionState{
		VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusUpcoming,
		LifecycleOrigin: OriginObserved, Present: true, LastSeenAt: storedSessionAt,
		Title: "stored title", TitleObservedAt: new(storedSessionAt),
		ScheduledStartTime: new(scheduled), ScheduleObservedAt: new(storedSessionAt),
		Clock: LiveEvidenceClock{LastUpcomingPositiveAt: new(storedSessionAt.Add(-time.Minute))},
	})

	fact := sessionFact("UPCOMING")

	fact.Title = "same-time title"
	fact.ScheduledAt = new(scheduled.Add(time.Hour))

	got := sessionOf(mustReduceAll(t, state, []Evidence{
		liveEvidence(2, storedSessionAt, contract.CompletenessComplete, contract.ContinuityContiguous, fact),
	}, 0))
	if got.Title != "stored title" || !got.ScheduledStartTime.Equal(scheduled) {
		t.Fatalf("same-time metadata replaced stored facts: title=%q schedule=%v", got.Title, got.ScheduledStartTime)
	}
}

func TestReduceNeverReturnsLiveSessionToUpcoming(t *testing.T) {
	t.Parallel()

	state := stateWithSession(&SessionState{
		VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusLive,
		LifecycleOrigin: OriginObserved, Present: true, LastSeenAt: storedSessionAt, StatusObservedAt: new(storedSessionAt),
		Clock: LiveEvidenceClock{LastLivePositiveAt: new(storedSessionAt), LastLivePositiveSeenAt: new(storedSessionAt)},
	})
	upcoming := liveEvidence(2, storedSessionAt.Add(time.Minute), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("UPCOMING"))

	got := sessionOf(mustReduceAll(t, state, []Evidence{upcoming}, time.Hour))
	if got.Status != domain.LiveStatusLive || !got.StatusObservedAt.Equal(storedSessionAt) {
		t.Fatalf("newer UPCOMING regressed LIVE: status=%s observed=%v", got.Status, got.StatusObservedAt)
	}
}

// 저장된 세션의 채널은 바뀌지 않고, 세션 행 없이 head만 있는 영상은 관측한 채널을 받는다.
func TestReduceKeepsStoredChannel(t *testing.T) {
	t.Parallel()

	stored := stateWithSession(&SessionState{
		VideoID: testVideoID, ChannelID: "UC_STORED", Status: domain.LiveStatusUpcoming,
		LifecycleOrigin: OriginObserved, Present: true, LastSeenAt: storedSessionAt,
		Clock: LiveEvidenceClock{LastUpcomingPositiveAt: new(storedSessionAt), LastUpcomingPositiveSeenAt: new(storedSessionAt)},
	})
	if got := sessionOf(mustReduceAll(t, stored, []Evidence{liveA()}, time.Hour)); got.ChannelID != "UC_STORED" {
		t.Fatalf("positive moved stored session to channel %q", got.ChannelID)
	}

	headOnly := stateWithSession(&SessionState{
		VideoID: testVideoID, Status: domain.LiveStatusUpcoming, HeadPresent: true,
		IgnoredAbsences: LoadedIgnoredAbsences(nil),
	})
	if got := sessionOf(mustReduceAll(t, headOnly, []Evidence{liveA()}, time.Hour)); got.ChannelID != testChannelID {
		t.Fatalf("head-only session channel = %q, want observed channel", got.ChannelID)
	}
}

// 종료는 저장된 세션의 ended_at을 유지하고 head에는 새 종료 시각을 기록한다.
func TestReduceEndKeepsStoredSessionEndedAt(t *testing.T) {
	t.Parallel()

	legacyEnd := storedSessionAt.Add(-time.Hour)
	positive := liveA()
	state := stateWithSession(&SessionState{
		VideoID: testVideoID, ChannelID: testChannelID, Status: domain.LiveStatusLive,
		LifecycleOrigin: OriginObserved, Present: true, HeadPresent: true, LastSeenAt: positive.ReceivedAt,
		EndedAt: new(legacyEnd), IgnoredAbsences: LoadedIgnoredAbsences(nil),
		Clock: LiveEvidenceClock{LastLivePositiveAt: new(positive.EffectiveAt), LastLivePositiveSeenAt: new(positive.ReceivedAt)},
	})
	end := endA()

	got := sessionOf(mustReduceAll(t, state, []Evidence{end}, 0))
	if got.Status != domain.LiveStatusEnded || !got.EndedAt.Equal(legacyEnd) ||
		got.Clock.EndedAt == nil || !got.Clock.EndedAt.Equal(end.EffectiveAt) {
		t.Fatalf("end = status %s session ended_at %v head ended_at %v", got.Status, got.EndedAt, got.Clock.EndedAt)
	}
}
