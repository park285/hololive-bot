package trigger_test

import (
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

func TestTriggerRouteContracts(t *testing.T) {
	t.Parallel()

	if triggercontracts.BasePath != "/internal/trigger" {
		t.Fatalf("BasePath = %q", triggercontracts.BasePath)
	}
	if triggercontracts.MajorEventWeeklyPath != "/internal/trigger/majorevent-weekly" {
		t.Fatalf("MajorEventWeeklyPath = %q", triggercontracts.MajorEventWeeklyPath)
	}
	if triggercontracts.MajorEventMonthlyPath != "/internal/trigger/majorevent-monthly" {
		t.Fatalf("MajorEventMonthlyPath = %q", triggercontracts.MajorEventMonthlyPath)
	}
	if triggercontracts.MemberNewsWeeklyPath != "/internal/trigger/membernews-weekly" {
		t.Fatalf("MemberNewsWeeklyPath = %q", triggercontracts.MemberNewsWeeklyPath)
	}
}
