package live

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Status = domain.LiveStatus

const (
	StatusUpcoming = domain.LiveStatusUpcoming
	StatusLive     = domain.LiveStatusLive
	StatusEnded    = domain.LiveStatusEnded
)

type EndEvidenceKind string

const (
	EndEvidenceExplicitEnd    EndEvidenceKind = "EXPLICIT_END"
	EndEvidenceExplicitCancel EndEvidenceKind = "EXPLICIT_CANCEL"
	EndEvidenceScopedAbsence  EndEvidenceKind = "SCOPED_ABSENCE"
)

type EndReason string

const (
	EndReasonExplicitEnd         EndReason = "EXPLICIT_END"
	EndReasonCancelledBeforeLive EndReason = "CANCELLED_BEFORE_LIVE"
	EndReasonScopedAbsence       EndReason = "SCOPED_ABSENCE"
)

type LiveEvidenceClock struct {
	LastUpcomingPositiveAt     *time.Time
	LastUpcomingPositiveSeenAt *time.Time
	LastLivePositiveAt         *time.Time
	LastLivePositiveSeenAt     *time.Time
	LastEndEvidenceAt          *time.Time
	LastCompleteAbsenceAt      *time.Time
	ConsecutiveAbsenceSlots    int
	EndCandidateKind           *EndEvidenceKind
	EndCandidateObservationID  *int64
	NextEndCheckAt             *time.Time
	EndedAt                    *time.Time
}

type EndEvidence struct {
	Kind                 EndEvidenceKind
	EffectiveAt          time.Time
	Valid                bool
	EntityMatchesSession bool
	NegativeEligible     bool
	ScopeCoversSession   bool
	HasPositiveAtOrAfter bool
}

type SessionFact struct {
	VideoID     string
	ChannelID   string
	Status      string
	ScheduledAt *time.Time
	StartedAt   *time.Time
	EndedAt     *time.Time
}

type SessionState struct {
	VideoID                    string
	ChannelID                  string
	Status                     Status
	Title                      string
	ScheduledStartTime         *time.Time
	StartedAt                  *time.Time
	EndedAt                    *time.Time
	LiveFirstSeenAt            *time.Time
	LastSeenAt                 time.Time
	Clock                      LiveEvidenceClock
	EndReason                  *EndReason
	FirstAbsenceScheduledFor   *time.Time
	SecondAbsenceScheduledFor  *time.Time
	LastAbsenceObservationID   int64
	LastAbsenceScheduledFor    *time.Time
	IgnoredAbsenceScheduledFor []time.Time
	Present                    bool
}

type AbsenceSlot struct {
	ScheduledFor   time.Time
	ObservationID  int64
	EvidenceSHA256 string
	EffectiveAt    time.Time
	ReceivedAt     time.Time
	ScopeSHA256    string
	Coverage       contract.GlobalChannelCoverageV1
}

type PendingEnd struct {
	Kind             EndEvidenceKind
	VideoID          string
	ChannelID        string
	ObservationID    int64
	EffectiveAt      time.Time
	ReceivedAt       time.Time
	ScheduledFor     time.Time
	EndedAt          *time.Time
	NegativeEligible bool
	ScopeCovers      bool
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
	Sessions       []SessionFact
	Coverage       contract.GlobalChannelCoverageV1
}

type State struct {
	Sessions     map[string]SessionState
	AbsenceSlots []AbsenceSlot
	PendingEnds  map[string]PendingEnd
}

type Application struct {
	EntityKind string
	EntityKey  string
	Decision   string
}

type Decision struct {
	Sessions     []SessionState
	PendingEnds  []PendingEnd
	AbsenceSlot  *AbsenceSlot
	Applications []Application
}

func (s State) clone() State {
	cloned := s
	cloned.Sessions = make(map[string]SessionState, len(s.Sessions))
	for key, value := range s.Sessions {
		if len(value.IgnoredAbsenceScheduledFor) > 0 {
			value.IgnoredAbsenceScheduledFor = append([]time.Time(nil), value.IgnoredAbsenceScheduledFor...)
		}
		cloned.Sessions[key] = value
	}
	if len(s.AbsenceSlots) > 0 {
		cloned.AbsenceSlots = append([]AbsenceSlot(nil), s.AbsenceSlots...)
	}
	cloned.PendingEnds = make(map[string]PendingEnd, len(s.PendingEnds))
	for key, value := range s.PendingEnds {
		cloned.PendingEnds[key] = value
	}
	return cloned
}
