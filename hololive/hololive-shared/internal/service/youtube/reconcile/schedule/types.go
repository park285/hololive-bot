package schedule

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Item struct {
	GroupKey           string
	Provider           contract.Provider
	ExternalID         string
	VideoID            string
	ChannelID          string
	Title              string
	ScheduledAt        time.Time
	EndedAt            *time.Time
	IsLive             bool
	CollaboTalentNames []string
}

type Session struct {
	VideoID            string
	ChannelID          string
	Status             domain.LiveStatus
	Title              string
	ScheduledStartTime *time.Time
	LastSeenAt         time.Time
}

type Evidence struct {
	ObservationID int64
	Provider      contract.Provider
	GroupKey      string
	Items         []Item
	EffectiveAt   time.Time
	ReceivedAt    time.Time
}

type State struct {
	Items    map[string]Item
	Sessions map[string]Session
}

type Application struct {
	EntityKind string
	EntityKey  string
	Decision   string
}

type Decision struct {
	Items        []Item
	Sessions     []Session
	Applications []Application
}

func ItemIdentity(provider contract.Provider, item *Item) string {
	if item.VideoID != "" {
		return "yt:" + item.VideoID
	}

	return "tmp:" + string(provider) + ":" + item.ExternalID
}

func (s *State) clone() State {
	cloned := *s

	cloned.Items = make(map[string]Item, len(s.Items))

	for key := range s.Items {
		item := s.Items[key]

		cloned.Items[key] = item.clone()
	}

	cloned.Sessions = make(map[string]Session, len(s.Sessions))
	for key := range s.Sessions {
		session := s.Sessions[key]

		cloned.Sessions[key] = session.clone()
	}

	return cloned
}

func (e *Evidence) clone() Evidence {
	cloned := *e

	cloned.Items = make([]Item, len(e.Items))

	for i := range e.Items {
		cloned.Items[i] = e.Items[i].clone()
	}

	return cloned
}

func (i *Item) clone() Item {
	cloned := *i

	cloned.EndedAt = cloneTime(i.EndedAt)
	cloned.CollaboTalentNames = cloneStrings(i.CollaboTalentNames)

	return cloned
}

func (s *Session) clone() Session {
	cloned := *s

	cloned.ScheduledStartTime = cloneTime(s.ScheduledStartTime)

	return cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}
