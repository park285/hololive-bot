package notifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

// dedup 저장소 오류는 "이미 선점됨"(skip)이 아니라 실패다. 이를 skip으로 세면 Valkey 장애 동안
// 알림이 조용히 사라지고 재시도 대상에서도 빠진다.
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
