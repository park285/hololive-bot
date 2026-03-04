package providers

import "testing"

func TestYouTubeStackNilSafeAccessors(t *testing.T) {
	t.Parallel()

	var stack *YouTubeStack
	if stack.GetService() != nil {
		t.Fatal("GetService() must return nil for nil receiver")
	}
	if stack.GetScheduler() != nil {
		t.Fatal("GetScheduler() must return nil for nil receiver")
	}
	if stack.GetStatsRepo() != nil {
		t.Fatal("GetStatsRepo() must return nil for nil receiver")
	}
}
