package settings

import "testing"

func TestScraperProxyApplyResultAsMap(t *testing.T) {
	t.Parallel()

	youtubeApplied := true
	pollersApplied := 3
	got := (ScraperProxyApplyResult{
		Requested:               true,
		YoutubeApplied:          &youtubeApplied,
		SchedulerPollersApplied: &pollersApplied,
	}).AsMap()

	if got["requested"] != true {
		t.Fatalf("requested key mismatch: %+v", got)
	}
	if got["youtube_applied"] != true {
		t.Fatalf("youtube_applied key mismatch: %+v", got)
	}
	if got["scheduler_pollers_applied"] != 3 {
		t.Fatalf("scheduler_pollers_applied key mismatch: %+v", got)
	}
	if _, exists := got["reason"]; exists {
		t.Fatalf("reason key should not exist: %+v", got)
	}
}

func TestMemberNewsWeeklyRunNowResultAsMap(t *testing.T) {
	t.Parallel()

	got := (MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "not configured",
	}).AsMap()

	if got["applied"] != false {
		t.Fatalf("applied key mismatch: %+v", got)
	}
	if got["reason"] != "not configured" {
		t.Fatalf("reason key mismatch: %+v", got)
	}
}
