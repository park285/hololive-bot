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
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
