package notification

import "testing"

func TestAlarmPersistTimeoutDoesNotExceedShutdownTimeout(t *testing.T) {
	if alarmPersistTaskTimeout > alarmServiceCloseTimeout {
		t.Fatalf("alarmPersistTaskTimeout(%v) must be <= alarmServiceCloseTimeout(%v)", alarmPersistTaskTimeout, alarmServiceCloseTimeout)
	}
}
