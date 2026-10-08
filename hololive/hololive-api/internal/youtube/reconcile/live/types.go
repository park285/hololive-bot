package live

import (
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// LifecycleOrigin은 일정 메타데이터와 실제 수명 관측의 출처를 구분한다.
type LifecycleOrigin string

const (
	OriginMetadataOnly  LifecycleOrigin = "metadata_only"
	OriginObserved      LifecycleOrigin = "observed"
	OriginLegacyUnknown LifecycleOrigin = "legacy_unknown"
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
	EndReasonUnresolvableVideo   EndReason = "UNRESOLVABLE_VIDEO"
)

// StatusUnresolvable은 익명 영상 확인이 identity를 확인할 수 없게 된 LIVE 영상의 사실이다.
// 비공개·삭제 전환 영상은 identity_missing만 남겨 명시적 종료도 부재 종료도 받을 수 없다.
const StatusUnresolvable = "UNRESOLVABLE"

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
	// UnresolvableSince는 마지막 LIVE positive 이후 영상 확인이 identity_missing으로만 이어진 첫 관측 시각이다.
	// positive가 지우며 UNRESOLVABLE_VIDEO 종료의 ended_at 하한이다.
	UnresolvableSince *time.Time
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
	VideoID            string
	ChannelID          string
	Status             string
	Title              string
	TopicID            string
	ThumbnailURL       string
	ScheduledAt        *time.Time
	StartedAt          *time.Time
	EndedAt            *time.Time
	LiveStartConfirmed bool
	VerifiedTerminal   bool
}

type SessionState struct {
	VideoID                   string
	ChannelID                 string
	Status                    domain.LiveStatus
	LifecycleOrigin           LifecycleOrigin
	Title                     string
	TopicID                   string
	ThumbnailURL              string
	ScheduledStartTime        *time.Time
	StartedAt                 *time.Time
	EndedAt                   *time.Time
	LiveFirstSeenAt           *time.Time
	LastSeenAt                time.Time
	IsPremiere                *bool
	StatusObservedAt          *time.Time
	ScheduleObservedAt        *time.Time
	TitleObservedAt           *time.Time
	Clock                     LiveEvidenceClock
	EndReason                 *EndReason
	FirstAbsenceScheduledFor  *time.Time
	SecondAbsenceScheduledFor *time.Time
	LastAbsenceObservationID  int64
	LastAbsenceScheduledFor   *time.Time
	IgnoredAbsences           IgnoredAbsenceHistory
	Present                   bool
	HeadPresent               bool
}

// IgnoredAbsenceHistory는 첫 LIVE positive 전에 무시한 부재 slot 예정 시각 목록이다.
// 0값은 "적재하지 않음"이다. 적재하지 않은 이력은 reducer가 읽거나 늘리지 않고 Reduce 오류로
// 드러내며, 저장 경로는 이를 SQL NULL로 보내 기존 DB 값을 유지한다. 0값을 빈 이력으로 두면
// 배열을 빼고 읽은 세션 하나만 저장해도 이력이 지워지므로 빈 이력은 LoadedIgnoredAbsences(nil)로만 만든다.
type IgnoredAbsenceHistory struct {
	slots  []time.Time
	loaded bool
}

// LoadedIgnoredAbsences는 적재했거나 새로 만든 세션의 이력이다. 인자 nil은 빈 이력이며
// 이 함수가 slots의 소유권을 넘겨받는다.
func LoadedIgnoredAbsences(slots []time.Time) IgnoredAbsenceHistory {
	return IgnoredAbsenceHistory{slots: slots, loaded: true}
}

// Slots는 이력과 적재 여부를 반환한다. 적재하지 않은 이력(false)을 빈 이력으로 해석하지 않는다.
// 반환 slice는 읽기 전용이다.
func (h *IgnoredAbsenceHistory) Slots() ([]time.Time, bool) {
	return slices.Clip(h.slots), h.loaded
}

// add는 reducer만 호출하며 호출 전에 적재 여부를 확인한다.
func (h *IgnoredAbsenceHistory) add(at time.Time) {
	h.slots = append(h.slots, at)
}

func (h *IgnoredAbsenceHistory) clone() IgnoredAbsenceHistory {
	return IgnoredAbsenceHistory{slots: slices.Clone(h.slots), loaded: h.loaded}
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
	cloned.StatusObservedAt = copyOptionalTime(s.StatusObservedAt)
	cloned.ScheduleObservedAt = copyOptionalTime(s.ScheduleObservedAt)
	cloned.TitleObservedAt = copyOptionalTime(s.TitleObservedAt)
	cloned.IsPremiere = cloneBool(s.IsPremiere)
	cloned.Clock.LastUpcomingPositiveAt = copyOptionalTime(s.Clock.LastUpcomingPositiveAt)
	cloned.Clock.LastUpcomingPositiveSeenAt = copyOptionalTime(s.Clock.LastUpcomingPositiveSeenAt)
	cloned.Clock.LastLivePositiveAt = copyOptionalTime(s.Clock.LastLivePositiveAt)
	cloned.Clock.LastLivePositiveSeenAt = copyOptionalTime(s.Clock.LastLivePositiveSeenAt)
	cloned.Clock.LastEndEvidenceAt = copyOptionalTime(s.Clock.LastEndEvidenceAt)
	cloned.Clock.LastCompleteAbsenceAt = copyOptionalTime(s.Clock.LastCompleteAbsenceAt)
	cloned.Clock.NextEndCheckAt = copyOptionalTime(s.Clock.NextEndCheckAt)
	cloned.Clock.EndedAt = copyOptionalTime(s.Clock.EndedAt)
	cloned.Clock.UnresolvableSince = copyOptionalTime(s.Clock.UnresolvableSince)
	cloned.FirstAbsenceScheduledFor = copyOptionalTime(s.FirstAbsenceScheduledFor)
	cloned.SecondAbsenceScheduledFor = copyOptionalTime(s.SecondAbsenceScheduledFor)
	cloned.LastAbsenceScheduledFor = copyOptionalTime(s.LastAbsenceScheduledFor)
	cloned.EndReason = cloneEndReason(s.EndReason)
	cloned.Clock.EndCandidateKind = cloneEndEvidenceKind(s.Clock.EndCandidateKind)
	cloned.Clock.EndCandidateObservationID = cloneInt64(s.Clock.EndCandidateObservationID)
	cloned.IgnoredAbsences = s.IgnoredAbsences.clone()

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

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}

	return new(*value)
}
