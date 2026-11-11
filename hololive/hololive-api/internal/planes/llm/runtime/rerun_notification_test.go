package runtime

import (
	"errors"
	"testing"
)

func TestRerunNotificationRejectsUnknownAndUnconfigured(t *testing.T) {
	t.Parallel()

	runtime := &LLMSchedulerRuntime{}

	if _, err := runtime.RerunNotification(t.Context(), "weekly"); !errors.Is(err, ErrUnknownNotification) {
		t.Fatalf("RerunNotification(unknown) error = %v, want ErrUnknownNotification", err)
	}

	for _, kind := range []string{NotificationMajorEventWeekly, NotificationMajorEventMonthly, NotificationMemberNewsWeekly} {
		if _, err := runtime.RerunNotification(t.Context(), kind); err == nil {
			t.Fatalf("RerunNotification(%s) without scheduler error = nil", kind)
		}
	}
}
