package dispatch

import "time"

func cloneUTCTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}

	normalized := value.UTC()
	return &normalized
}
