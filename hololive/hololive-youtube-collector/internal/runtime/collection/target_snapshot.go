package collection

import (
	"fmt"
	"slices"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

// Lease subject와 target subject의 저장 상한이며, 저장소의 job identity 검증과 같은 값입니다.
const maxSubjectBytes = 256

// TargetSnapshot은 lease가 고정한 projection 세대의 수집 대상 시야입니다.
// 생성자가 정렬·중복·상한 불변식을 검증하며 조회는 방어적 복사본을 반환합니다.
type TargetSnapshot struct {
	generation   int64
	membership   JobMembership
	exactSubject string
	requested    []contract.ObservationKind
	subjects     map[contract.ObservationKind][]string
}

// NewExactTargetSnapshot은 한 subject의 요청 kind별 활성 여부로 EXACT_SUBJECT 시야를 만듭니다.
func NewExactTargetSnapshot(
	generation int64,
	subject string,
	requested []contract.ObservationKind,
	enabled map[contract.ObservationKind]bool,
) (TargetSnapshot, error) {
	kinds, err := validateSnapshotIdentity(generation, JobMembershipExactSubject, subject, requested)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("validate snapshot identity: %w", err)
	}

	subjects := make(map[contract.ObservationKind][]string, len(kinds))
	for _, kind := range kinds {
		subjects[kind] = []string{}
		if enabled[kind] {
			subjects[kind] = []string{subject}
		}
	}

	for kind := range enabled {
		if _, ok := subjects[kind]; !ok {
			return TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("exact snapshot contains an unrequested kind"))
		}
	}

	return TargetSnapshot{
		generation: generation, membership: JobMembershipExactSubject,
		exactSubject: subject, requested: kinds, subjects: subjects,
	}, nil
}

// NewProjectionTargetSnapshot은 요청 kind별 roster로 CURRENT_PROJECTION 시야를 만들고 전체 행 수를 maxRows로 제한합니다.
func NewProjectionTargetSnapshot(
	generation int64,
	requested []contract.ObservationKind,
	rows map[contract.ObservationKind][]string,
	maxRows int,
) (TargetSnapshot, error) {
	kinds, err := validateSnapshotIdentity(generation, JobMembershipCurrentProjection, "", requested)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("validate snapshot identity: %w", err)
	}

	if maxRows < 1 {
		return TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("projection snapshot roster cap is invalid"))
	}

	subjects, err := projectionSnapshotSubjects(kinds, rows, maxRows)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("projection snapshot subjects: %w", err)
	}

	for kind := range rows {
		if _, ok := subjects[kind]; !ok {
			return TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("projection snapshot contains an unrequested kind"))
		}
	}

	return TargetSnapshot{
		generation: generation, membership: JobMembershipCurrentProjection,
		requested: kinds, subjects: subjects,
	}, nil
}

func projectionSnapshotSubjects(
	kinds []contract.ObservationKind,
	rows map[contract.ObservationKind][]string,
	maxRows int,
) (map[contract.ObservationKind][]string, error) {
	subjects := make(map[contract.ObservationKind][]string, len(kinds))
	total := 0

	for _, kind := range kinds {
		values, ok := rows[kind]
		if !ok {
			return nil, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("projection snapshot is missing a requested kind"))
		}

		values = cloneSnapshotSubjects(values)
		slices.Sort(values)

		if err := validateProjectionSubjects(values); err != nil {
			return nil, fmt.Errorf("validate projection subjects: %w", err)
		}

		total += len(values)
		if total > maxRows {
			return nil, collecterr.New(collecterr.TargetRosterTooLarge, collecterr.ClassResourceLimit, "target roster exceeds configured limit")
		}

		subjects[kind] = values
	}

	return subjects, nil
}

func validateProjectionSubjects(values []string) error {
	for index, subject := range values {
		if invalidSubject(subject) {
			return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("projection snapshot subject is invalid"))
		}

		if index > 0 && values[index-1] == subject {
			return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("projection snapshot contains a duplicate subject"))
		}
	}

	return nil
}

func validateSnapshotIdentity(
	generation int64,
	membership JobMembership,
	exactSubject string,
	requested []contract.ObservationKind,
) ([]contract.ObservationKind, error) {
	if invalidSnapshotIdentity(generation, membership, requested) {
		return nil, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot identity is invalid"))
	}

	if membership == JobMembershipExactSubject && invalidSubject(exactSubject) {
		return nil, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("exact snapshot subject is invalid"))
	}

	kinds := cloneSnapshotKinds(requested)
	slices.Sort(kinds)

	if err := validateSnapshotKinds(kinds); err != nil {
		return nil, fmt.Errorf("validate snapshot kinds: %w", err)
	}

	return kinds, nil
}

func invalidSnapshotIdentity(
	generation int64,
	membership JobMembership,
	requested []contract.ObservationKind,
) bool {
	return generation <= 0 || !membership.Valid() || len(requested) == 0
}

func validateSnapshotKinds(kinds []contract.ObservationKind) error {
	for index, kind := range kinds {
		if !kind.Valid() {
			return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot contains an invalid kind"))
		}

		if index > 0 && kinds[index-1] == kind {
			return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot contains a duplicate requested kind"))
		}
	}

	return nil
}

// invalidSubject는 공백으로 둘러싸였거나 비었거나 maxSubjectBytes를 넘는 subject를 거부합니다.
func invalidSubject(value string) bool {
	return strings.TrimSpace(value) != value || value == "" || len(value) > maxSubjectBytes
}

func snapshotInvariant(message string) error {
	return collecterr.New(collecterr.Internal, collecterr.ClassInternal, message)
}

func (s TargetSnapshot) Generation() int64 {
	return s.generation
}

func (s TargetSnapshot) Membership() JobMembership {
	return s.membership
}

func (s TargetSnapshot) RequestedKinds() []contract.ObservationKind {
	return cloneSnapshotKinds(s.requested)
}

func (s TargetSnapshot) Allows(kind contract.ObservationKind, subject string) (bool, error) {
	if !kind.Valid() || invalidSubject(subject) {
		return false, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot lookup is invalid"))
	}

	subjects, ok := s.subjects[kind]
	if !ok {
		return false, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot kind is missing"))
	}

	_, found := slices.BinarySearch(subjects, subject)

	return found, nil
}

func (s TargetSnapshot) Roster(kind contract.ObservationKind) ([]string, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot roster kind is invalid"))
	}

	subjects, ok := s.subjects[kind]
	if !ok {
		return nil, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot roster kind is missing"))
	}

	return cloneSnapshotSubjects(subjects), nil
}

func (s TargetSnapshot) ValidateRequested(kinds []contract.ObservationKind) error {
	requested, err := validateSnapshotIdentity(s.generation, s.membership, s.exactSubject, kinds)
	if err != nil {
		return fmt.Errorf("validate snapshot identity: %w", err)
	}

	if !slices.Equal(requested, s.requested) {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot requested kinds do not match"))
	}

	for _, kind := range requested {
		if _, ok := s.subjects[kind]; !ok {
			return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot is missing a requested kind"))
		}
	}

	return nil
}

func (s TargetSnapshot) Clone() TargetSnapshot {
	subjects := make(map[contract.ObservationKind][]string, len(s.subjects))
	for kind, values := range s.subjects {
		subjects[kind] = cloneSnapshotSubjects(values)
	}

	return TargetSnapshot{
		generation: s.generation, membership: s.membership, exactSubject: s.exactSubject,
		requested: cloneSnapshotKinds(s.requested), subjects: subjects,
	}
}

func cloneSnapshotKinds(values []contract.ObservationKind) []contract.ObservationKind {
	cloned := make([]contract.ObservationKind, len(values))
	copy(cloned, values)

	return cloned
}

func cloneSnapshotSubjects(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}
