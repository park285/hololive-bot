package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type noveltyVerdict int

const (
	noveltyIneligible noveltyVerdict = iota
	noveltyUndecided
	noveltyEligible
)

func setEarliestBaseline(state *State, evidence *Evidence) {
	if evidence.Kind != contract.KindVideoList || (len(evidence.Videos) == 0 && !completeEligible(evidence)) {
		return
	}

	if state.EarliestBaselineAt == nil || evidence.EffectiveAt.Before(*state.EarliestBaselineAt) {
		state.EarliestBaselineAt = new(evidence.EffectiveAt.UTC())
	}
}

func noveltyBaseline(state *State) *time.Time {
	switch {
	case state.EarliestCompleteAt == nil:
		return state.EarliestBaselineAt
	case state.EarliestBaselineAt == nil || state.EarliestCompleteAt.Before(*state.EarliestBaselineAt):
		return state.EarliestCompleteAt
	default:
		return state.EarliestBaselineAt
	}
}

func decideVideoNovelty(session *reduceSession) {
	baseline := noveltyBaseline(session.state)

	for i := range session.evidence.Videos {
		entity := &session.evidence.Videos[i]

		state, ok := session.state.Videos[entity.VideoID]
		if !ok || conflicted(session, entity.VideoID) {
			continue
		}

		_, firstSeen := session.applied[entity.VideoID]
		if !firstSeen && !state.NoveltyPending {
			continue
		}

		verdict := videoNovelty(session.state, baseline, state.FirstPositiveEffectiveAt, entity)

		state.NoveltyPending = verdict == noveltyUndecided
		session.state.Videos[entity.VideoID] = state

		if verdict == noveltyEligible {
			markNovel(session, *entity)
		}
	}
}

func videoNovelty(state *State, baseline *time.Time, firstPositiveAt time.Time, entity *Entity) noveltyVerdict {
	if _, known := state.KnownElsewhere[entity.VideoID]; known {
		return noveltyIneligible
	}

	if baseline == nil || !firstPositiveAt.After(*baseline) {
		return noveltyIneligible
	}

	publication := entity.Publication
	if publication == nil {
		return noveltyUndecided
	}

	switch publication.Status {
	case contract.VideoPublicationPublished:
		if publication.PublishedAt == nil || publication.PublishedAt.After(publication.CheckedAt) {
			return noveltyUndecided
		}

		if publication.PublishedAt.After(*baseline) {
			return noveltyEligible
		}

		return noveltyIneligible
	case contract.VideoPublicationUpcomingPremiere:
		if publication.ScheduledFor == nil {
			return noveltyUndecided
		}

		if publication.ScheduledFor.After(*baseline) {
			return noveltyEligible
		}

		return noveltyIneligible
	case contract.VideoPublicationUnresolved:
		return noveltyUndecided
	default:
		return noveltyUndecided
	}
}

func conflicted(session *reduceSession, videoID string) bool {
	for i := range session.conflicts {
		if session.conflicts[i].VideoID == videoID {
			return true
		}
	}

	return false
}

func markNovel(session *reduceSession, entity Entity) {
	if session.novel == nil {
		session.novel = map[string]Entity{}
	}

	session.novel[entity.VideoID] = entity
}
