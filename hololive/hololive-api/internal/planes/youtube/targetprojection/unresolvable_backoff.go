package targetprojection

import "time"

const (
	unresolvableBackoffFirstStep  = 10 * time.Minute
	unresolvableBackoffSecondStep = time.Hour
	unresolvableBackoffThirdStep  = 24 * time.Hour
)

func unresolvablePollInterval(base, unresolvedFor time.Duration) time.Duration {
	switch {
	case unresolvedFor < unresolvableBackoffFirstStep:
		return base
	case unresolvedFor < unresolvableBackoffSecondStep:
		return 5 * base
	case unresolvedFor < unresolvableBackoffThirdStep:
		return 15 * base
	default:
		return 30 * base
	}
}
