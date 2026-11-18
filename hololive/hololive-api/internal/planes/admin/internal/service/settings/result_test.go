package settings

import (
	"testing"
)

func TestSettingsRuntimeStateResultAsMap(t *testing.T) {
	t.Parallel()

	got := (SettingsRuntimeStateResult{AlarmTargetMinutes: []int{5, 1}}).AsMap()

	if minutes, ok := got["alarm_target_minutes"].([]int); !ok || len(minutes) != 2 {
		t.Fatalf("alarm_target_minutes key mismatch: %+v", got)
	}
}
