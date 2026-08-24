package sourceobservation

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func cutoffOrNil(now time.Time, age time.Duration) (any, bool) {
	if age <= 0 {
		return nil, false
	}

	return now.Add(-age), true
}

func evidencePolicies(ages map[contract.ObservationKind]time.Duration, now time.Time) ([]string, []time.Time) {
	kinds := make([]string, 0, len(ages))
	cutoffs := make([]time.Time, 0, len(ages))

	for kind, age := range ages {
		if age <= 0 || !kind.Valid() {
			continue
		}

		kinds = append(kinds, string(kind))
		cutoffs = append(cutoffs, now.Add(-age))
	}

	return kinds, cutoffs
}

func applicationAuditPolicies(
	ages map[contract.ObservationKind]time.Duration,
	grace time.Duration,
	now time.Time,
) ([]string, []time.Time) {
	if grace <= 0 {
		return nil, nil
	}

	kinds := make([]string, 0, len(ages))
	cutoffs := make([]time.Time, 0, len(ages))

	for kind, age := range ages {
		if age <= 0 || !kind.Valid() {
			continue
		}

		kinds = append(kinds, string(kind))
		cutoffs = append(cutoffs, now.Add(-(age + grace)))
	}

	return kinds, cutoffs
}

func minPositiveDuration(values ...time.Duration) time.Duration {
	var minimum time.Duration

	for _, value := range values {
		if value <= 0 {
			continue
		}

		if minimum == 0 || value < minimum {
			minimum = value
		}
	}

	return minimum
}

func minEvidenceAge(ages map[contract.ObservationKind]time.Duration) time.Duration {
	var minimum time.Duration

	for _, age := range ages {
		if age <= 0 {
			continue
		}

		if minimum == 0 || age < minimum {
			minimum = age
		}
	}

	return minimum
}

func minApplicationAuditAge(cfg RetentionConfig) time.Duration {
	evidenceAge := minEvidenceAge(cfg.EvidenceAgeByKind)
	if evidenceAge <= 0 || cfg.ApplicationAuditGrace <= 0 {
		return 0
	}

	return evidenceAge + cfg.ApplicationAuditGrace
}
