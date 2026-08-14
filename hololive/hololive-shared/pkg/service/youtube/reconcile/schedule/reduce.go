package schedule

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func Reduce(state State, evidence Evidence) (Decision, error) {
	if evidence.GroupKey == "" {
		return Decision{}, fmt.Errorf("schedule reducer received empty group key")
	}
	current := state.clone()
	if current.Items == nil {
		current.Items = map[string]Item{}
	}
	if current.Sessions == nil {
		current.Sessions = map[string]Session{}
	}
	items := make([]Item, 0, len(evidence.Items))
	sessions := make([]Session, 0)
	applications := make([]Application, 0, len(evidence.Items))
	for i := range evidence.Items {
		item := evidence.Items[i]
		item.Provider = evidence.Provider
		item.GroupKey = evidence.GroupKey
		key := ItemIdentity(evidence.Provider, item)
		current.Items[key] = item
		items = append(items, item)
		applications = append(applications, Application{
			EntityKind: "youtube_schedule_item", EntityKey: key, Decision: "APPLIED",
		})
		if session, ok := mergeSession(current, item); ok {
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

func mergeSession(state State, item Item) (Session, bool) {
	if item.VideoID == "" {
		return Session{}, false
	}
	existing, ok := state.Sessions[item.VideoID]
	if !ok {
		if item.ChannelID == "" {
			return Session{}, false
		}
		scheduled := item.ScheduledAt.UTC()
		return Session{
			VideoID:            item.VideoID,
			ChannelID:          item.ChannelID,
			Status:             domain.LiveStatusUpcoming,
			Title:              item.Title,
			ScheduledStartTime: &scheduled,
			LastSeenAt:         scheduled,
		}, true
	}
	if existing.Status == domain.LiveStatusEnded {
		return Session{}, false
	}
	if item.Title != "" {
		existing.Title = item.Title
	}
	if item.ChannelID != "" && existing.ChannelID == "" {
		existing.ChannelID = item.ChannelID
	}
	scheduled := item.ScheduledAt.UTC()
	existing.ScheduledStartTime = &scheduled
	return existing, true
}
