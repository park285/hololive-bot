package notification

import (
	"errors"
	"testing"
)

func TestAlarmOperationResult(t *testing.T) {
	t.Parallel()

	if got := alarmOperationResult(nil); got != "ok" {
		t.Fatalf("alarmOperationResult(nil) = %q, want %q", got, "ok")
	}

	if got := alarmOperationResult(errors.New("boom")); got != "error" {
		t.Fatalf("alarmOperationResult(err) = %q, want %q", got, "error")
	}
}
