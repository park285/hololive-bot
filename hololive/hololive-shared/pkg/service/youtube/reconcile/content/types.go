package content

import (
	"maps"
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
	IsShort      bool
}

type EntityState struct {
	Entity
	Clock                     ContentEvidenceClock
	FirstPositiveEffectiveAt  time.Time
	LastPositiveValueSHA256   string
	LastPositiveScopeSHA256   string
	LastPositiveCoverage      coverageValue
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
	Coverage       coverageValue
}

type coverageValue struct {
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
	Coverage       coverageValue
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

func (s State) clone() State {
	cloned := s
	cloned.Videos = make(map[string]EntityState, len(s.Videos))
	maps.Copy(cloned.Videos, s.Videos)
	if len(s.AbsenceSlots) > 0 {
		cloned.AbsenceSlots = append([]AbsenceSlot(nil), s.AbsenceSlots...)
	}
	if s.EarliestCompleteAt != nil {
		earliest := *s.EarliestCompleteAt
		cloned.EarliestCompleteAt = &earliest
	}
	return cloned
}

func VideoCoverage(value contract.ChannelListCoverageV1) coverageValue {
	copied := value
	return coverageValue{Videos: &copied}
}

func ShortsCoverage(value contract.ShortsListCoverageV1) coverageValue {
	copied := value
	return coverageValue{Shorts: &copied}
}
