package keys

import (
	"testing"
)

func TestH5_KeyConstantValuePins(t *testing.T) {
	t.Parallel()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"NotifyClaimKeyPrefix", NotifyClaimKeyPrefix, "notified:claim:"},
		{"NotifyLogicalClaimKeyPrefix", NotifyLogicalClaimKeyPrefix, "notified:claim:event:"},
		{"NotifiedKeyPrefix", NotifiedKeyPrefix, "notified:"},
		{"UpcomingEventKeyPrefix", UpcomingEventKeyPrefix, "notified:upcoming:event:"},
		{"ScheduleTransitionKeyPrefix", ScheduleTransitionKeyPrefix, "notified:schedule:transition:"},
		{"DispatchQueueKey", DispatchQueueKey, "alarm:dispatch:queue"},
		{"DispatchRetryQueueKey", DispatchRetryQueueKey, "alarm:dispatch:retry"},
		{"DispatchDLQKey", DispatchDLQKey, "alarm:dispatch:dlq"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
