package tracking

import (
	"strings"
	"time"
)

func observationComparisonTitleHintKey(value string) string {
	return strings.ToLower(observationComparisonNormalizeTitleHint(value))
}

func timeValueForObservationPostComparison(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
