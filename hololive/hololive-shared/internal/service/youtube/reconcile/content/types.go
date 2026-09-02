package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type ContentEvidenceClock struct {
	LastPositiveEffectiveAt time.Time
	LastPositiveReceivedAt  time.Time
	LastNegativeEffectiveAt *time.Time
	MissingSinceEffectiveAt *time.Time
}

type Entity struct {
	VideoID      string
	ChannelID    string
	Title        string
	PublishedAt  *time.Time
	ScheduledFor *time.Time
	IsPremiere   *bool
	IsShort      bool
}

type EntityState struct {
	Entity

	Clock                     ContentEvidenceClock
	FirstPositiveEffectiveAt  time.Time
	LastPositiveValueSHA256   string
	LastPositiveScopeSHA256   string
	LastPositiveCoverage      CoverageValue
	LastNegativeReceivedAt    *time.Time
	FirstAbsenceScheduledFor  *time.Time
	SecondAbsenceScheduledFor *time.Time
	LastAbsenceObservationID  int64
	ConsecutiveAbsenceSlots   int
	WithdrawnAt               *time.Time
}

type AbsenceSlot struct {
	ScheduledFor   time.Time
	ObservationID  int64
	EvidenceSHA256 string
	EffectiveAt    time.Time
	ReceivedAt     time.Time
	ScopeSHA256    string
	Coverage       CoverageValue
}

type CoverageValue struct {
	Videos *contract.ChannelListCoverageV1
	Shorts *contract.ShortsListCoverageV1
}

type Evidence struct {
	Kind           contract.ObservationKind
	ObservationID  int64
	ObservationKey string
	EvidenceSHA256 string
	ScopeSHA256    string
	ScheduledFor   time.Time
	EffectiveAt    time.Time
	ReceivedAt     time.Time
	Completeness   contract.Completeness
	Continuity     contract.Continuity
	Videos         []Entity
	Coverage       CoverageValue
}

type State struct {
	ChannelID          string
	Kind               contract.ObservationKind
	Initialized        bool
	LastContentID      string
	EarliestCompleteAt *time.Time
	Videos             map[string]EntityState
	AbsenceSlots       []AbsenceSlot
}

type NotificationIntent struct {
	Kind      domain.OutboxKind
	ChannelID string
	ContentID string
	Video     Entity
}

type Conflict struct {
	VideoID              string
	FieldName            string
	ExistingValueSHA256  string
	AttemptedValueSHA256 string
}

type Application struct {
	EntityKind string
	EntityKey  string
	Decision   string
}

type Decision struct {
	Videos             []Entity
	FieldUpdates       []Entity
	Notifications      []NotificationIntent
	Tracking           []NotificationIntent
	Watermark          *domain.YouTubeContentWatermark
	Clocks             []EntityState
	AbsenceSlot        *AbsenceSlot
	EarliestCompleteAt *time.Time
	Conflicts          []Conflict
	Applications       []Application
}

func (s *State) clone() State {
	cloned := *s

	cloned.Videos = make(map[string]EntityState, len(s.Videos))

	for videoID := range s.Videos {
		entity := s.Videos[videoID]

		cloned.Videos[videoID] = entity.clone()
	}

	if len(s.AbsenceSlots) > 0 {
		cloned.AbsenceSlots = make([]AbsenceSlot, len(s.AbsenceSlots))
		for i := range s.AbsenceSlots {
			cloned.AbsenceSlots[i] = s.AbsenceSlots[i].clone()
		}
	}

	if s.EarliestCompleteAt != nil {
		earliest := *s.EarliestCompleteAt

		cloned.EarliestCompleteAt = &earliest
	}

	return cloned
}

func (e *Evidence) clone() Evidence {
	cloned := *e
	if len(e.Videos) > 0 {
		cloned.Videos = make([]Entity, len(e.Videos))
		for i := range e.Videos {
			cloned.Videos[i] = e.Videos[i].clone()
		}
	}

	cloned.Coverage = e.Coverage.clone()

	return cloned
}

func (e *EntityState) clone() EntityState {
	cloned := *e

	cloned.Entity = e.Entity.clone()
	cloned.Clock.LastNegativeEffectiveAt = cloneTime(e.Clock.LastNegativeEffectiveAt)
	cloned.Clock.MissingSinceEffectiveAt = cloneTime(e.Clock.MissingSinceEffectiveAt)
	cloned.LastPositiveCoverage = e.LastPositiveCoverage.clone()
	cloned.LastNegativeReceivedAt = cloneTime(e.LastNegativeReceivedAt)
	cloned.FirstAbsenceScheduledFor = cloneTime(e.FirstAbsenceScheduledFor)
	cloned.SecondAbsenceScheduledFor = cloneTime(e.SecondAbsenceScheduledFor)
	cloned.WithdrawnAt = cloneTime(e.WithdrawnAt)

	return cloned
}

func (e Entity) clone() Entity {
	cloned := e

	cloned.PublishedAt = cloneTime(e.PublishedAt)
	cloned.ScheduledFor = cloneTime(e.ScheduledFor)
	cloned.IsPremiere = cloneBool(e.IsPremiere)

	return cloned
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func (s *AbsenceSlot) clone() AbsenceSlot {
	cloned := *s

	cloned.Coverage = s.Coverage.clone()

	return cloned
}

func (c CoverageValue) clone() CoverageValue {
	cloned := c
	if c.Videos != nil {
		videos := *c.Videos

		videos.Filters.PublishedAfter = cloneTime(c.Videos.Filters.PublishedAfter)
		videos.Filters.PublishedBefore = cloneTime(c.Videos.Filters.PublishedBefore)
		cloned.Videos = &videos
	}

	if c.Shorts != nil {
		shorts := *c.Shorts

		cloned.Shorts = &shorts
	}

	return cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	copied := *value

	return &copied
}

func VideoCoverage(value *contract.ChannelListCoverageV1) CoverageValue {
	if value == nil {
		return CoverageValue{}
	}

	copied := *value

	return CoverageValue{Videos: &copied}
}

func ShortsCoverage(value *contract.ShortsListCoverageV1) CoverageValue {
	if value == nil {
		return CoverageValue{}
	}

	copied := *value

	return CoverageValue{Shorts: &copied}
}
