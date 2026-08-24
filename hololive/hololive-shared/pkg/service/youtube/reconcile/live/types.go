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
	EndReasonCancelledBeforeLive EndReason = "CANCELLED_BEFORE_LIVE" //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
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

func (s *State) clone() State {
	cloned := *s

	cloned.Sessions = make(map[string]SessionState, len(s.Sessions))

	for key := range s.Sessions {
		value := s.Sessions[key]

		cloned.Sessions[key] = value.clone()
	}

	if len(s.AbsenceSlots) > 0 {
		cloned.AbsenceSlots = make([]AbsenceSlot, len(s.AbsenceSlots))
		for i := range s.AbsenceSlots {
			cloned.AbsenceSlots[i] = s.AbsenceSlots[i].clone()
		}
	}

	cloned.PendingEnds = make(map[string]PendingEnd, len(s.PendingEnds))
	for key := range s.PendingEnds {
		value := s.PendingEnds[key]

		cloned.PendingEnds[key] = value.clone()
	}

	return cloned
}

func (e *Evidence) clone() Evidence {
	cloned := *e
	if len(e.Sessions) > 0 {
		cloned.Sessions = make([]SessionFact, len(e.Sessions))
		for i := range e.Sessions {
			cloned.Sessions[i] = e.Sessions[i].clone()
		}
	}

	cloned.Coverage = cloneCoverage(&e.Coverage)

	return cloned
}

func (s *SessionFact) clone() SessionFact {
	cloned := *s

	cloned.ScheduledAt = copyOptionalTime(s.ScheduledAt)
	cloned.StartedAt = copyOptionalTime(s.StartedAt)
	cloned.EndedAt = copyOptionalTime(s.EndedAt)

	return cloned
}

func (s *SessionState) clone() SessionState {
	cloned := *s

	cloned.ScheduledStartTime = copyOptionalTime(s.ScheduledStartTime)
	cloned.StartedAt = copyOptionalTime(s.StartedAt)
	cloned.EndedAt = copyOptionalTime(s.EndedAt)
	cloned.LiveFirstSeenAt = copyOptionalTime(s.LiveFirstSeenAt)
	cloned.Clock.LastUpcomingPositiveAt = copyOptionalTime(s.Clock.LastUpcomingPositiveAt)
	cloned.Clock.LastUpcomingPositiveSeenAt = copyOptionalTime(s.Clock.LastUpcomingPositiveSeenAt)
	cloned.Clock.LastLivePositiveAt = copyOptionalTime(s.Clock.LastLivePositiveAt)
	cloned.Clock.LastLivePositiveSeenAt = copyOptionalTime(s.Clock.LastLivePositiveSeenAt)
	cloned.Clock.LastEndEvidenceAt = copyOptionalTime(s.Clock.LastEndEvidenceAt)
	cloned.Clock.LastCompleteAbsenceAt = copyOptionalTime(s.Clock.LastCompleteAbsenceAt)
	cloned.Clock.NextEndCheckAt = copyOptionalTime(s.Clock.NextEndCheckAt)
	cloned.Clock.EndedAt = copyOptionalTime(s.Clock.EndedAt)
	cloned.FirstAbsenceScheduledFor = copyOptionalTime(s.FirstAbsenceScheduledFor)
	cloned.SecondAbsenceScheduledFor = copyOptionalTime(s.SecondAbsenceScheduledFor)
	cloned.LastAbsenceScheduledFor = copyOptionalTime(s.LastAbsenceScheduledFor)
	cloned.EndReason = cloneEndReason(s.EndReason)
	cloned.Clock.EndCandidateKind = cloneEndEvidenceKind(s.Clock.EndCandidateKind)
	cloned.Clock.EndCandidateObservationID = cloneInt64(s.Clock.EndCandidateObservationID)
	cloned.IgnoredAbsenceScheduledFor = append([]time.Time(nil), s.IgnoredAbsenceScheduledFor...)

	return cloned
}

func (s *AbsenceSlot) clone() AbsenceSlot {
	cloned := *s

	cloned.Coverage = cloneCoverage(&s.Coverage)

	return cloned
}

func (p *PendingEnd) clone() PendingEnd {
	cloned := *p

	cloned.EndedAt = copyOptionalTime(p.EndedAt)

	return cloned
}

func cloneCoverage(value *contract.GlobalChannelCoverageV1) contract.GlobalChannelCoverageV1 {
	cloned := *value

	cloned.RequestedChannelIDs = append([]string(nil), value.RequestedChannelIDs...)
	cloned.Filters.Statuses = append([]string(nil), value.Filters.Statuses...)

	return cloned
}

func cloneEndReason(value *EndReason) *EndReason {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func cloneEndEvidenceKind(value *EndEvidenceKind) *EndEvidenceKind {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
