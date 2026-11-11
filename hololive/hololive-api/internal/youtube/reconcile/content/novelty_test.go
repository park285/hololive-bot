package content

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	noveltyBaselineAt = time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	noveltyLaterAt    = time.Date(2026, time.August, 14, 1, 5, 0, 0, time.UTC)
	noveltyLatestAt   = time.Date(2026, time.August, 14, 1, 10, 0, 0, time.UTC)
)

func unanchoredState() *State {
	return &State{ChannelID: testChannelID, Kind: contract.KindVideoList, Initialized: true, Videos: map[string]EntityState{}}
}

func partialList(id int64, at time.Time, entities ...Entity) Evidence {
	evidence := positiveAt(id, at, Entity{})

	evidence.Videos = entities
	evidence.Completeness = contract.CompletenessPartial
	evidence.Continuity = contract.ContinuityGapUnresolved
	evidence.Coverage = VideoCoverage(&contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10})

	return evidence
}

func unresolvedEntity(videoID string) Entity {
	return Entity{VideoID: videoID, ChannelID: testChannelID, Title: videoID}
}

func premiereEntity(videoID string, scheduled time.Time) Entity {
	return Entity{
		VideoID: videoID, ChannelID: testChannelID, Title: videoID, ScheduledFor: new(scheduled), IsPremiere: new(true),
		Publication: &contract.VideoPublicationV1{
			Status: contract.VideoPublicationUpcomingPremiere, ScheduledFor: new(scheduled), CheckedAt: noveltyLaterAt,
		},
	}
}

func reduceStep(t *testing.T, state *State, evidence Evidence) (*Decision, State) {
	t.Helper()

	decision, err := Reduce(*state, evidence, 0)
	if err != nil {
		t.Fatal(err)
	}

	return &decision, stateFromDecision(state, &decision, &evidence)
}

func TestNoveltyFirstPartialListIsSilentBaseline(t *testing.T) {
	t.Parallel()

	baseline, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt,
		publishedEntity("baseline-new", "B", noveltyBaselineAt.Add(-time.Minute)), unresolvedEntity("baseline-unknown")))
	assertNotifications(t, baseline)

	if baseline.EarliestBaselineAt == nil || !baseline.EarliestBaselineAt.Equal(noveltyBaselineAt) || baseline.EarliestCompleteAt != nil {
		t.Fatalf("baseline=%v complete=%v", baseline.EarliestBaselineAt, baseline.EarliestCompleteAt)
	}

	if state.Videos["baseline-unknown"].NoveltyPending {
		t.Fatal("baseline item must be decided silent, not pending")
	}

	upload := publishedEntity("upload", "U", noveltyBaselineAt.Add(2*time.Minute))
	next, _ := reduceStep(t, &state, partialList(2, noveltyLaterAt, upload, unresolvedEntity("baseline-unknown")))
	assertNotifications(t, next, "upload")

	if next.Notifications[0].Kind != domain.OutboxKindNewVideo || next.EarliestCompleteAt != nil {
		t.Fatalf("notification=%#v complete=%v", next.Notifications[0], next.EarliestCompleteAt)
	}
}

func TestNoveltyEmptyPartialDoesNotSetBaselineButCompleteEmptyDoes(t *testing.T) {
	t.Parallel()

	empty, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt))
	if empty.EarliestBaselineAt != nil {
		t.Fatalf("empty partial set baseline %v", empty.EarliestBaselineAt)
	}

	complete, state := reduceStep(t, &state, emptyAt(2, noveltyBaselineAt, contract.CompletenessComplete, wideCoverage()))
	if complete.EarliestBaselineAt == nil || complete.EarliestCompleteAt == nil {
		t.Fatalf("complete empty baseline=%v complete=%v", complete.EarliestBaselineAt, complete.EarliestCompleteAt)
	}

	upload, _ := reduceStep(t, &state, partialList(3, noveltyLaterAt, publishedEntity("upload", "U", noveltyBaselineAt.Add(time.Minute))))
	assertNotifications(t, upload, "upload")
}

func TestNoveltyOldNeverSeenVideoStaysSilent(t *testing.T) {
	t.Parallel()

	old := publishedEntity("old", "O", time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
	decision, state := reduceStep(t, seededState(), partialList(1, noveltyBaselineAt, old))
	assertNotifications(t, decision)

	if state.Videos["old"].NoveltyPending {
		t.Fatal("old publication must resolve, not wait")
	}
}

func TestNoveltyLateEvidenceDecidesPendingCandidateOnce(t *testing.T) {
	t.Parallel()

	_, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt, unresolvedEntity("known")))

	future := publishedEntity("late", "L", noveltyLatestAt)

	future.Publication.CheckedAt = noveltyLaterAt

	pending, state := reduceStep(t, &state, partialList(2, noveltyLaterAt, future))
	assertNotifications(t, pending)

	if !state.Videos["late"].NoveltyPending {
		t.Fatal("future publication must keep the candidate pending")
	}

	resolved, state := reduceStep(t, &state, partialList(3, noveltyLatestAt, publishedEntity("late", "L", noveltyLaterAt)))
	assertNotifications(t, resolved, "late")

	if state.Videos["late"].NoveltyPending {
		t.Fatal("decided candidate stayed pending")
	}

	again, _ := reduceStep(t, &state, partialList(4, noveltyLatestAt.Add(time.Minute), publishedEntity("late", "L", noveltyLaterAt)))
	assertNotifications(t, again)
}

func TestNoveltyPendingCandidateSeenBeforeBaselineResolvesSilently(t *testing.T) {
	t.Parallel()

	_, state := reduceStep(t, unanchoredState(), partialList(1, noveltyLaterAt, unresolvedEntity("known")))

	_, state = reduceStep(t, &state, partialList(2, noveltyLatestAt, unresolvedEntity("pending")))

	if !state.Videos["pending"].NoveltyPending {
		t.Fatal("candidate without evidence must wait")
	}

	older, state := reduceStep(t, &state, partialList(3, noveltyBaselineAt, publishedEntity("pending", "P", noveltyLatestAt)))
	assertNotifications(t, older)

	if state.Videos["pending"].NoveltyPending {
		t.Fatal("candidate first seen at the earlier baseline must resolve silently")
	}
}

func TestNoveltyUpcomingPremiereStaysSilentThroughRelease(t *testing.T) {
	t.Parallel()

	_, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt, unresolvedEntity("known")))

	scheduled := noveltyLatestAt.Add(time.Hour)
	discovered, state := reduceStep(t, &state, partialList(2, noveltyLaterAt,
		premiereEntity("premiere", scheduled), publishedEntity("upload", "U", noveltyBaselineAt.Add(2*time.Minute))))
	assertNotifications(t, discovered, "upload")

	if state.Videos["premiere"].NoveltyPending {
		t.Fatal("upcoming premiere novelty must be decided at discovery")
	}

	released, _ := reduceStep(t, &state, partialList(3, noveltyLatestAt, publishedEntity("premiere", "premiere", scheduled)))
	assertNotifications(t, released)
}

func TestNoveltyBaselinePremiereStaysSilentThroughRelease(t *testing.T) {
	t.Parallel()

	scheduled := noveltyLatestAt.Add(time.Hour)
	baseline, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt, premiereEntity("premiere", scheduled)))
	assertNotifications(t, baseline)

	released, _ := reduceStep(t, &state, partialList(2, noveltyLatestAt, publishedEntity("premiere", "premiere", scheduled)))
	assertNotifications(t, released)
}

func TestNoveltyKnownElsewhereAndLegacyVideosStaySilent(t *testing.T) {
	t.Parallel()

	_, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt, unresolvedEntity("known")))

	state.KnownElsewhere = map[string]struct{}{"short-id": {}}
	state.Videos["legacy"] = EntityState{VideoID: "legacy", ChannelID: testChannelID, Title: "legacy"}

	decision, next := reduceStep(t, &state, partialList(2, noveltyLaterAt,
		publishedEntity("short-id", "S", noveltyLaterAt), publishedEntity("legacy", "legacy", noveltyLaterAt)))
	assertNotifications(t, decision)

	if next.Videos["short-id"].NoveltyPending || next.Videos["legacy"].NoveltyPending {
		t.Fatal("known videos must not wait for novelty evidence")
	}
}

func TestNoveltyListWithoutPublicationNeverNotifiesAlone(t *testing.T) {
	t.Parallel()

	legacy := Entity{VideoID: "legacy-payload", ChannelID: testChannelID, Title: "L", PublishedAt: new(noveltyLaterAt)}
	decision, state := reduceStep(t, seededState(), partialList(1, noveltyLaterAt, legacy))
	assertNotifications(t, decision)

	if !state.Videos["legacy-payload"].NoveltyPending {
		t.Fatal("candidate first seen after the anchor without evidence must wait")
	}
}

func TestNoveltyEqualTimeReplayIsSilent(t *testing.T) {
	t.Parallel()

	_, state := reduceStep(t, unanchoredState(), partialList(1, noveltyBaselineAt, unresolvedEntity("known")))
	upload := publishedEntity("upload", "U", noveltyLaterAt)

	first, state := reduceStep(t, &state, partialList(2, noveltyLaterAt, upload))
	assertNotifications(t, first, "upload")

	replay, _ := reduceStep(t, &state, partialList(2, noveltyLaterAt, upload))
	assertNotifications(t, replay)
}
