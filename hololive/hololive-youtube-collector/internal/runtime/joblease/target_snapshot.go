package joblease

import (
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type TargetSnapshot struct {
	generation   int64
	membership   sourceobservation.JobMembership
	exactSubject string
	requested    []contract.ObservationKind
	subjects     map[contract.ObservationKind][]string
}

func newExactTargetSnapshot(
	generation int64,
	subject string,
	requested []contract.ObservationKind,
	enabled map[contract.ObservationKind]bool,
) (TargetSnapshot, error) {
	kinds, err := validateSnapshotIdentity(generation, sourceobservation.JobMembershipExactSubject, subject, requested)
	if err != nil {
		return TargetSnapshot{}, err
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
			return TargetSnapshot{}, snapshotInvariant("exact snapshot contains an unrequested kind")
		}
	}
	return TargetSnapshot{
		generation: generation, membership: sourceobservation.JobMembershipExactSubject,
		exactSubject: subject, requested: kinds, subjects: subjects,
	}, nil
}

func newProjectionTargetSnapshot(
	generation int64,
	requested []contract.ObservationKind,
	rows map[contract.ObservationKind][]string,
	maxRows int,
) (TargetSnapshot, error) {
	kinds, err := validateSnapshotIdentity(generation, sourceobservation.JobMembershipCurrentProjection, "", requested)
	if err != nil || maxRows < 1 {
		if err != nil {
			return TargetSnapshot{}, err
		}
		return TargetSnapshot{}, snapshotInvariant("projection snapshot roster cap is invalid")
	}
	subjects, err := projectionSnapshotSubjects(kinds, rows, maxRows)
	if err != nil {
		return TargetSnapshot{}, err
	}
	for kind := range rows {
		if _, ok := subjects[kind]; !ok {
			return TargetSnapshot{}, snapshotInvariant("projection snapshot contains an unrequested kind")
		}
	}
	return TargetSnapshot{
		generation: generation, membership: sourceobservation.JobMembershipCurrentProjection,
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
			return nil, snapshotInvariant("projection snapshot is missing a requested kind")
		}
		values = cloneSnapshotSubjects(values)
		slices.Sort(values)
		if err := validateProjectionSubjects(values); err != nil {
			return nil, err
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
		if invalidBoundedToken(subject, 256) {
			return snapshotInvariant("projection snapshot subject is invalid")
		}
		if index > 0 && values[index-1] == subject {
			return snapshotInvariant("projection snapshot contains a duplicate subject")
		}
	}
	return nil
}

func validateSnapshotIdentity(
	generation int64,
	membership sourceobservation.JobMembership,
	exactSubject string,
	requested []contract.ObservationKind,
) ([]contract.ObservationKind, error) {
	if invalidSnapshotIdentity(generation, membership, requested) {
		return nil, snapshotInvariant("target snapshot identity is invalid")
	}
	if membership == sourceobservation.JobMembershipExactSubject && invalidBoundedToken(exactSubject, 256) {
		return nil, snapshotInvariant("exact snapshot subject is invalid")
	}
	kinds := cloneSnapshotKinds(requested)
	slices.Sort(kinds)
	if err := validateSnapshotKinds(kinds); err != nil {
		return nil, err
	}
	return kinds, nil
}

func invalidSnapshotIdentity(
	generation int64,
	membership sourceobservation.JobMembership,
	requested []contract.ObservationKind,
) bool {
	return generation <= 0 || !membership.Valid() || len(requested) == 0
}

func validateSnapshotKinds(kinds []contract.ObservationKind) error {
	for index, kind := range kinds {
		if !kind.Valid() {
			return snapshotInvariant("target snapshot contains an invalid kind")
		}
		if index > 0 && kinds[index-1] == kind {
			return snapshotInvariant("target snapshot contains a duplicate requested kind")
		}
	}
	return nil
}

func snapshotInvariant(message string) error {
	return collecterr.New(collecterr.Internal, collecterr.ClassInternal, message)
}

func (s TargetSnapshot) Generation() int64 {
	return s.generation
}

func (s TargetSnapshot) Membership() sourceobservation.JobMembership {
	return s.membership
}

func (s TargetSnapshot) RequestedKinds() []contract.ObservationKind {
	return cloneSnapshotKinds(s.requested)
}

func (s TargetSnapshot) Allows(kind contract.ObservationKind, subject string) (bool, error) {
	if !kind.Valid() || invalidBoundedToken(subject, 256) {
		return false, snapshotInvariant("target snapshot lookup is invalid")
	}
	subjects, ok := s.subjects[kind]
	if !ok {
		return false, snapshotInvariant("target snapshot kind is missing")
	}
	_, found := slices.BinarySearch(subjects, subject)
	return found, nil
}

func (s TargetSnapshot) Roster(kind contract.ObservationKind) ([]string, error) {
	if !kind.Valid() {
		return nil, snapshotInvariant("target snapshot roster kind is invalid")
	}
	subjects, ok := s.subjects[kind]
	if !ok {
		return nil, snapshotInvariant("target snapshot roster kind is missing")
	}
	return cloneSnapshotSubjects(subjects), nil
}

func (s TargetSnapshot) ValidateRequested(kinds []contract.ObservationKind) error {
	requested, err := validateSnapshotIdentity(s.generation, s.membership, s.exactSubject, kinds)
	if err != nil {
		return err
	}
	if !slices.Equal(requested, s.requested) {
		return snapshotInvariant("target snapshot requested kinds do not match")
	}
	for _, kind := range requested {
		if _, ok := s.subjects[kind]; !ok {
			return snapshotInvariant("target snapshot is missing a requested kind")
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

func ExpectedLeaseSubject(job sourceobservation.JobContract, dynamicSubject string) (string, error) {
	if err := job.Validate(); err != nil {
		return "", fmt.Errorf("build collection job identity: %w: canonical job contract is invalid", ErrInvalidJob)
	}
	subject := dynamicSubject
	if job.Class() == sourceobservation.JobClassGlobal {
		subject = job.LeaseSubject()
	}
	if invalidBoundedToken(subject, 256) {
		return "", fmt.Errorf("build collection job identity: %w: subject is outside bounds", ErrInvalidJob)
	}
	return subject, nil
}

func BuildJobKey(id sourceobservation.JobID, subject string) (string, error) {
	if !id.Valid() || invalidBoundedToken(subject, 256) {
		return "", fmt.Errorf("build collection job key: %w: identity is outside bounds", ErrInvalidJob)
	}
	definition, ok := sourceobservation.InitialJobContracts().Definition(id)
	if !ok {
		return "", fmt.Errorf("build collection job key: %w: canonical job contract is missing", ErrInvalidJob)
	}
	suffix := subject
	if definition.Class() == sourceobservation.JobClassGlobal {
		if subject != definition.LeaseSubject() {
			return "", fmt.Errorf("build collection job key: %w: global lease subject mismatch", ErrInvalidJob)
		}
		suffix = "global"
	}
	key := "collector:" + string(id.Provider) + ":" + string(id.Kind) + ":" + suffix
	if invalidBoundedToken(key, 512) {
		return "", fmt.Errorf("build collection job key: %w: key is outside bounds", ErrInvalidJob)
	}
	return key, nil
}
