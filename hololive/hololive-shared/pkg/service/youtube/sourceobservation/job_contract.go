package sourceobservation

import (
	"fmt"
	"slices"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type JobKind string
type JobClass string
type JobMembership string

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
		return JobContract{}, err
	}
	normalizedCadence, err := normalizeObservationKinds("cadence", cadenceKinds)
	if err != nil {
		return JobContract{}, err
	}
	normalizedRoster, err := normalizeObservationKinds("roster", rosterKinds)
	if err != nil {
		return JobContract{}, err
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
		return JobContract{}, err
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
		return fmt.Errorf("validate job contract: identity is invalid")
	}
	if !j.definition.class.Valid() || !j.definition.membership.Valid() {
		return fmt.Errorf("validate job contract: class or membership is invalid")
	}
	if err := validateClassMembership(
		j.definition.class,
		j.definition.membership,
		j.definition.leaseSubject,
		j.definition.rosterKinds,
	); err != nil {
		return err
	}
	if err := validateKindSlices(j.definition.emissions, j.definition.cadenceKinds, j.definition.rosterKinds); err != nil {
		return err
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
		return validateSubjectMembership(membership, leaseSubject)
	case JobClassGlobal:
		return validateGlobalMembership(membership, leaseSubject, roster)
	default:
		return fmt.Errorf("validate job contract: class is invalid")
	}
}

func validateSubjectMembership(membership JobMembership, leaseSubject string) error {
	if membership != JobMembershipExactSubject {
		return fmt.Errorf("validate job contract: SUBJECT requires EXACT_SUBJECT")
	}
	if leaseSubject != "" {
		return fmt.Errorf("validate job contract: SUBJECT lease subject must be empty")
	}
	return nil
}

func validateGlobalMembership(
	membership JobMembership,
	leaseSubject string,
	roster []contract.ObservationKind,
) error {
	if leaseSubject == "" || strings.TrimSpace(leaseSubject) != leaseSubject || len(leaseSubject) > 256 {
		return fmt.Errorf("validate job contract: GLOBAL lease subject is invalid")
	}
	if membership == JobMembershipCurrentProjection && len(roster) == 0 {
		return fmt.Errorf("validate job contract: CURRENT_PROJECTION requires a roster kind")
	}
	return nil
}

func validateKindSlices(
	emissions []contract.ObservationKind,
	cadence []contract.ObservationKind,
	roster []contract.ObservationKind,
) error {
	cadenceSet := make(map[contract.ObservationKind]struct{}, len(cadence))
	for _, kind := range cadence {
		cadenceSet[kind] = struct{}{}
	}
	for _, kind := range emissions {
		if _, ok := cadenceSet[kind]; !ok {
			return fmt.Errorf("validate job contract: emission %q is missing from cadence", kind)
		}
	}
	_ = roster
	return nil
}

func unionObservationKinds(left, right []contract.ObservationKind) []contract.ObservationKind {
	seen := make(map[contract.ObservationKind]struct{}, len(left)+len(right))
	out := make([]contract.ObservationKind, 0, len(left)+len(right))
	for _, kind := range append(slices.Clone(left), right...) {
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

func leaseSubjectMismatch(job JobContract, subject string) bool {
	return job.Membership() == JobMembershipExactSubject && job.LeaseSubject() != "" && job.LeaseSubject() != subject
}

func compareJobID(a, b JobID) int {
	if a.Provider != b.Provider {
		if a.Provider < b.Provider {
			return -1
		}
		return 1
	}
	if a.Kind == b.Kind {
		return 0
	}
	if a.Kind < b.Kind {
		return -1
	}
	return 1
}
