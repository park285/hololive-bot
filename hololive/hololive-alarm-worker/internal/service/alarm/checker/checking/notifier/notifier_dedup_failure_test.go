package notifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestPrepareOneReportsDedupStoreErrorAsFailed(t *testing.T) {
	t.Parallel()

	cacheClient := &cachemocks.Client{
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return false, errors.New("valkey unreachable")
		},
	}
	notifier := &Notifier{dedupService: dedup.NewService(cacheClient, []int{5, 3, 1}, newCheckerTestLogger())}

	start := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	stream := &domain.Stream{
		ID:             "youtube-stream-1",
		Title:          "테스트 방송",
		ChannelID:      "UC_TEST",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: "UC_TEST", Name: "테스트 채널"},
	}
	notification := domain.NewAlarmNotification("room1", stream.Channel, stream, 5, []string{}, "")

	prepared, err := notifier.prepareOne(t.Context(), notification)
	if err == nil {
		t.Fatal("prepareOne() error = nil, want dedup store error")
	}

	if prepared.outcome != sendOutcomeFailed {
		t.Fatalf("prepareOne() outcome = %v, want failed", prepared.outcome)
	}
}
