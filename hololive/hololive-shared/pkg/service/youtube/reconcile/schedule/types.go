package schedule

import (
	"maps"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Item struct {
	GroupKey    string
	Provider    contract.Provider
	ExternalID  string
	VideoID     string
	ChannelID   string
	Title       string
	ScheduledAt time.Time
	EndedAt     *time.Time
	IsLive      bool
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

func ItemIdentity(provider contract.Provider, item Item) string {
	if item.VideoID != "" {
		return "yt:" + item.VideoID
	}
	return "tmp:" + string(provider) + ":" + item.ExternalID
}

func (s State) clone() State {
	cloned := s
	cloned.Items = make(map[string]Item, len(s.Items))
	maps.Copy(cloned.Items, s.Items)
	cloned.Sessions = make(map[string]Session, len(s.Sessions))
	maps.Copy(cloned.Sessions, s.Sessions)
	return cloned
}
