package streamcommon

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFindByChannelAndScheduledMinute(t *testing.T) {
	scheduled := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	streams := []*domain.Stream{
		{ChannelID: "channel-a", StartScheduled: &scheduled},
	}

	candidate := &domain.Stream{
		ChannelID:      "channel-a",
		StartScheduled: &scheduled,
	}

	got := FindByChannelAndScheduledMinute(streams, candidate)
	if got == nil {
		t.Fatal("expected matching stream")
	}
}
