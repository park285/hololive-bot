package notifier

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPrepareOneRejectsPersistenceInvalidIdentityBeforeDedup(t *testing.T) {
	t.Parallel()

	start := time.Now().UTC().Add(time.Hour)
	notification := &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-1",
		Stream: &domain.Stream{
			ID:             strings.Repeat("s", 65),
			ChannelID:      "UC_channel",
			StartScheduled: &start,
		},
	}

	_, _, outcome, err := (&Notifier{}).prepareOne(context.Background(), notification)
	if err == nil {
		t.Fatal("prepareOne() error = nil")
	}
	if outcome != sendOutcomeFailed {
		t.Fatalf("prepareOne() outcome = %v, want failed", outcome)
	}
}
