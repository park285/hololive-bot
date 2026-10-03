package schedule

import (
	"errors"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func Reduce(state State, evidence Evidence) (Decision, error) {
	if evidence.GroupKey == "" {
		return Decision{}, errors.New("schedule reducer received empty group key")
	}

	current := state.clone()
	workingEvidence := evidence.clone()

	if current.Sessions == nil {
		current.Sessions = map[string]Session{}
	}

	items := make([]Item, 0, len(workingEvidence.Items))
	sessions := make([]Session, 0)
	applications := make([]Application, 0, len(workingEvidence.Items))

	for i := range workingEvidence.Items {
		item := &workingEvidence.Items[i]

		item.Provider = workingEvidence.Provider
		item.GroupKey = workingEvidence.GroupKey

		key := ItemIdentity(workingEvidence.Provider, item)

		items = append(items, *item)
		applications = append(applications, Application{
			EntityKind: "youtube_schedule_item", EntityKey: key, Decision: "APPLIED",
		})

		if session, ok := mergeSession(&current, item, &workingEvidence); ok {
			current.Sessions[session.VideoID] = session
			sessions = append(sessions, session)
			applications = append(applications, Application{
				EntityKind: "youtube_live_session", EntityKey: session.VideoID, Decision: "SCHEDULE_MERGED",
			})
		}
	}

	if len(applications) > 1000 {
		applications = applications[:1000]
	}

	return Decision{Items: items, Sessions: sessions, Applications: applications}, nil
}

func mergeSession(state *State, item *Item, evidence *Evidence) (Session, bool) {
	if item.VideoID == "" {
		return Session{}, false
	}

	existing, ok := state.Sessions[item.VideoID]
	if !ok {
		if item.ChannelID == "" {
			return Session{}, false
		}

		scheduled := item.ScheduledAt.UTC()

		created := Session{
			VideoID:            item.VideoID,
			ChannelID:          item.ChannelID,
			Status:             domain.LiveStatusUpcoming,
			Title:              item.Title,
			ScheduledStartTime: &scheduled,
			LastSeenAt:         evidence.ReceivedAt.UTC(),
			ScheduleObservedAt: new(evidence.EffectiveAt.UTC()),
		}
		if item.Title != "" {
			created.TitleObservedAt = new(evidence.EffectiveAt.UTC())
		}

		return created, true
	}

	if existing.Status == domain.LiveStatusEnded {
		return Session{}, false
	}

	// 서로 다른 원천도 필드별 실제 관측 시각으로 비교한다. 같은 시각의 충돌은 기존 정본을 유지한다.
	changed := false

	if item.Title != "" && (existing.TitleObservedAt == nil || evidence.EffectiveAt.After(*existing.TitleObservedAt)) {
		existing.Title = item.Title
		existing.TitleObservedAt = new(evidence.EffectiveAt.UTC())
		changed = true
	}

	if item.ChannelID != "" && existing.ChannelID == "" {
		existing.ChannelID = item.ChannelID
		changed = true
	}

	if existing.ScheduleObservedAt == nil || evidence.EffectiveAt.After(*existing.ScheduleObservedAt) {
		existing.ScheduledStartTime = new(item.ScheduledAt.UTC())
		existing.ScheduleObservedAt = new(evidence.EffectiveAt.UTC())
		changed = true
	}

	return existing, changed
}
