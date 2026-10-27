package runtime

import (
	"errors"
	"testing"
)

// 알 수 없는 kind와 구성되지 않은 scheduler는 아무것도 적재하지 않고 오류로 끝난다.
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
