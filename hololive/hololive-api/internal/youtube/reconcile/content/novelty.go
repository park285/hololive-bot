package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// noveltyVerdict는 video_list 영상 하나의 NEW_VIDEO 적격성입니다.
type noveltyVerdict int

const (
	// 기준 목록·기존 영상·기준 이전 공개처럼 알리지 않기로 확정한 경우입니다.
	noveltyIneligible noveltyVerdict = iota
	// 기준 이후 처음 봤지만 결정적인 근거가 없어 보류하는 경우입니다.
	noveltyUndecided
	// 신뢰 가능한 공개·예정 근거가 기준보다 늦은 경우입니다.
	noveltyEligible
)

// setEarliestBaseline은 비어 있지 않거나 COMPLETE인 video_list의 effective 시각을 채널 기준으로 남깁니다.
// 빈 부분 목록은 기준이 아닙니다. 더 이른 관측이 늦게 처리되면 더 이른 시각으로 내려갑니다.
func setEarliestBaseline(state *State, evidence *Evidence) {
	if evidence.Kind != contract.KindVideoList || (len(evidence.Videos) == 0 && !completeEligible(evidence)) {
		return
	}

	if state.EarliestBaselineAt == nil || evidence.EffectiveAt.Before(*state.EarliestBaselineAt) {
		state.EarliestBaselineAt = new(evidence.EffectiveAt.UTC())
	}
}

// noveltyBaseline은 complete 근거와 기준 목록 중 이른 시각입니다. 둘 다 같은 채널의 수락된 목록 시각이므로
// 이른 쪽 이후에 처음 본 영상만 신규 후보입니다.
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

// decideVideoNovelty는 이번 관측에서 처음 본 영상과 근거를 기다리던 영상만 판정합니다.
// 이미 판정을 마친 영상은 근거가 바뀌어도 다시 알리지 않습니다. 값 충돌로 반영하지 않은 항목은 판정하지 않습니다.
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

// videoNovelty는 신규성 근거 확보 우선 정책입니다. 다른 채널·Shorts로 이미 저장된 영상, 기준 목록 이전·당시에 처음 본 영상은
// 알리지 않습니다. 기준 이후 처음 본 영상은 player 공개 시각이 기준보다 늦고 확인 시각보다 늦지 않거나, live 콘텐츠가 아닌
// 예정 Premiere의 확인된 예정 시각이 기준보다 늦을 때만 알립니다. 근거가 없거나 시각을 확정할 수 없으면 보류합니다.
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
