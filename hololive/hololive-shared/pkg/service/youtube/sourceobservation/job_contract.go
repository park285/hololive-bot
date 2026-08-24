package sourceobservation

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type (
	JobKind       string
	JobClass      string
	JobMembership string
)

const (
	JobClassSubject JobClass = "SUBJECT"
	JobClassGlobal  JobClass = "GLOBAL"

	JobMembershipExactSubject      JobMembership = "EXACT_SUBJECT"
	JobMembershipCurrentProjection JobMembership = "CURRENT_PROJECTION"
)

type JobID struct {
	Provider contract.Provider
	Kind     JobKind
}

func (id JobID) Valid() bool {
	return id.Provider.Valid() && jobKindValid(id.Kind)
}

func (id JobID) String() string {
	return string(id.Provider) + "/" + string(id.Kind)
}

func (class JobClass) Valid() bool {
	return class == JobClassSubject || class == JobClassGlobal
}

func (membership JobMembership) Valid() bool {
	return membership == JobMembershipExactSubject || membership == JobMembershipCurrentProjection
}

type JobContract struct {
	definition *jobContractDefinition
}

type jobContractDefinition struct {
	id           JobID
	class        JobClass
	membership   JobMembership
	leaseSubject string
	emissions    []contract.ObservationKind
	cadenceKinds []contract.ObservationKind
	rosterKinds  []contract.ObservationKind
}

func NewJobContract(
	id JobID,
	class JobClass,
	membership JobMembership,
	leaseSubject string,
	emissions []contract.ObservationKind,
	cadenceKinds []contract.ObservationKind,
	rosterKinds []contract.ObservationKind,
) (JobContract, error) {
	normalizedEmissions, err := normalizeObservationKinds("emission", emissions)
	if err != nil {
		return JobContract{}, fmt.Errorf("normalize observation kinds: %w", err)
	}

	normalizedCadence, err := normalizeObservationKinds("cadence", cadenceKinds)
	if err != nil {
		return JobContract{}, fmt.Errorf("normalize observation kinds: %w", err)
	}

	normalizedRoster, err := normalizeObservationKinds("roster", rosterKinds)
	if err != nil {
		return JobContract{}, fmt.Errorf("normalize observation kinds: %w", err)
	}

	job := JobContract{definition: &jobContractDefinition{
		id:           id,
		class:        class,
		membership:   membership,
		leaseSubject: leaseSubject,
		emissions:    normalizedEmissions,
		cadenceKinds: normalizedCadence,
		rosterKinds:  normalizedRoster,
	}}
	if err := job.Validate(); err != nil {
		return JobContract{}, fmt.Errorf("validate: %w", err)
	}

	return job, nil
}

func (j JobContract) ID() JobID {
	if j.definition == nil {
		return JobID{}
	}

	return j.definition.id
}

func (j JobContract) Class() JobClass {
	if j.definition == nil {
		return ""
	}

	return j.definition.class
}

func (j JobContract) Membership() JobMembership {
	if j.definition == nil {
		return ""
	}

	return j.definition.membership
}

func (j JobContract) LeaseSubject() string {
	if j.definition == nil {
		return ""
	}

	return j.definition.leaseSubject
}

func (j JobContract) Emissions() []contract.ObservationKind {
	if j.definition == nil {
		return []contract.ObservationKind{}
	}

	return cloneObservationKinds(j.definition.emissions)
}

func (j JobContract) CadenceKinds() []contract.ObservationKind {
	if j.definition == nil {
		return []contract.ObservationKind{}
	}

	return cloneObservationKinds(j.definition.cadenceKinds)
}

func (j JobContract) RosterKinds() []contract.ObservationKind {
	if j.definition == nil {
		return []contract.ObservationKind{}
	}

	return cloneObservationKinds(j.definition.rosterKinds)
}

func (j JobContract) RequestedKinds() []contract.ObservationKind {
	if j.definition == nil {
		return []contract.ObservationKind{}
	}

	return unionObservationKinds(j.definition.cadenceKinds, j.definition.rosterKinds)
}

func (j JobContract) Emits(kind contract.ObservationKind) bool {
	return j.definition != nil && slices.Contains(j.definition.emissions, kind)
}

func (j JobContract) UsesRoster(kind contract.ObservationKind) bool {
	return j.definition != nil && slices.Contains(j.definition.rosterKinds, kind)
}

func (j JobContract) Validate() error {
	if j.definition == nil || !j.definition.id.Valid() {
		return errors.New("validate job contract: identity is invalid")
	}

	if !j.definition.class.Valid() || !j.definition.membership.Valid() {
		return errors.New("validate job contract: class or membership is invalid")
	}

	if err := validateClassMembership(
		j.definition.class,
		j.definition.membership,
		j.definition.leaseSubject,
		j.definition.rosterKinds,
	); err != nil {
		return fmt.Errorf("validate class membership: %w", err)
	}

	if err := validateKindSlices(j.definition.emissions, j.definition.cadenceKinds, j.definition.rosterKinds); err != nil {
		return fmt.Errorf("validate kind slices: %w", err)
	}

	return nil
}

func (j JobContract) Clone() JobContract {
	if j.definition == nil {
		return JobContract{}
	}

	return JobContract{definition: &jobContractDefinition{
		id:           j.definition.id,
		class:        j.definition.class,
		membership:   j.definition.membership,
		leaseSubject: j.definition.leaseSubject,
		emissions:    cloneObservationKinds(j.definition.emissions),
		cadenceKinds: cloneObservationKinds(j.definition.cadenceKinds),
		rosterKinds:  cloneObservationKinds(j.definition.rosterKinds),
	}}
}

type JobContractSet interface {
	Definition(JobID) (JobContract, bool)
	IDs() []JobID
	Allows(JobID, contract.ObservationKind) bool
}

type StaticJobContracts map[JobID]JobContract

func (s StaticJobContracts) Definition(id JobID) (JobContract, bool) {
	definition, ok := s[id]
	if !ok {
		return JobContract{}, false
	}

	return definition.Clone(), true
}

func (s StaticJobContracts) IDs() []JobID {
	ids := make([]JobID, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}

	slices.SortFunc(ids, compareJobID)

	return ids
}

func (s StaticJobContracts) Allows(id JobID, kind contract.ObservationKind) bool {
	definition, ok := s.Definition(id)
	return ok && definition.Emits(kind)
}

func jobKindValid(kind JobKind) bool {
	value := string(kind)
	return value != "" && strings.TrimSpace(value) == value && !strings.Contains(value, "/") && len(value) <= 128
}

func normalizeObservationKinds(name string, kinds []contract.ObservationKind) ([]contract.ObservationKind, error) {
	if len(kinds) == 0 {
		return []contract.ObservationKind{}, nil
	}

	out := make([]contract.ObservationKind, 0, len(kinds))
	seen := make(map[contract.ObservationKind]struct{}, len(kinds))

	for _, kind := range kinds {
		if !kind.Valid() {
			return nil, fmt.Errorf("new job contract: invalid %s kind %q", name, kind)
		}

		if _, ok := seen[kind]; ok {
			return nil, fmt.Errorf("new job contract: duplicate %s kind %q", name, kind)
		}

		seen[kind] = struct{}{}
		out = append(out, kind)
	}

	slices.Sort(out)

	return out, nil
}

func cloneObservationKinds(kinds []contract.ObservationKind) []contract.ObservationKind {
	out := slices.Clone(kinds)
	if out == nil {
		return []contract.ObservationKind{}
	}

	return out
}

func validateClassMembership(class JobClass, membership JobMembership, leaseSubject string, roster []contract.ObservationKind) error {
	switch class {
	case JobClassSubject:
		return errors.Join(validateSubjectClassMembership(membership, leaseSubject))
	case JobClassGlobal:
		return errors.Join(validateGlobalClassMembership(membership, leaseSubject, roster))
	default:
		return errors.New("validate job contract: class is invalid")
	}
}

func validateSubjectClassMembership(membership JobMembership, leaseSubject string) error {
	if err := validateSubjectMembership(membership, leaseSubject); err != nil {
		return fmt.Errorf("validate subject membership: %w", err)
	}

	return nil
}

func validateGlobalClassMembership(membership JobMembership, leaseSubject string, roster []contract.ObservationKind) error {
	if err := validateGlobalMembership(membership, leaseSubject, roster); err != nil {
		return fmt.Errorf("validate global membership: %w", err)
	}

	return nil
}

func validateSubjectMembership(membership JobMembership, leaseSubject string) error {
	if membership != JobMembershipExactSubject {
		return errors.New("validate job contract: SUBJECT requires EXACT_SUBJECT")
	}

	if leaseSubject != "" {
		return errors.New("validate job contract: SUBJECT lease subject must be empty")
	}

	return nil
}
