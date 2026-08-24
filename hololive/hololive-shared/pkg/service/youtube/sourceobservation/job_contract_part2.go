package sourceobservation

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func validateGlobalMembership(
	membership JobMembership,
	leaseSubject string,
	roster []contract.ObservationKind,
) error {
	if leaseSubject == "" || strings.TrimSpace(leaseSubject) != leaseSubject || len(leaseSubject) > 256 {
		return errors.New("validate job contract: GLOBAL lease subject is invalid")
	}

	if membership == JobMembershipCurrentProjection && len(roster) == 0 {
		return errors.New("validate job contract: CURRENT_PROJECTION requires a roster kind")
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
