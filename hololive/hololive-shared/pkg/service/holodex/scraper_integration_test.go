package holodex

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// TestScraperLiveIntegration: 실제 schedule.hololive.tv 사이트에서 스크래핑이 동작하는지 확인합니다.
// 이 테스트는 -tags=integration 플래그로만 실행됩니다.
func TestScraperLiveIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := &ScraperService{
		httpClient: &http.Client{
			Timeout: constants.OfficialScheduleConfig.Timeout,
		},
		logger:        logger,
		baseURL:       constants.OfficialScheduleConfig.BaseURL,
		memberNameMap: make(map[string]string),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	streams, err := svc.fetchAllStreams(ctx)
	if err != nil {
		t.Fatalf("fetchAllStreams 실패: %v", err)
	}

	if len(streams) == 0 {
		t.Fatal("스트림을 찾지 못함 - HTML 구조가 변경되었을 수 있음")
	}

	t.Logf("스크래핑 성공: %d 개의 스트림을 찾음", len(streams))

	// 첫 번째 스트림 정보 검증
	for i, stream := range streams {
		if i >= 5 {
			break
		}

		t.Logf("   스트림 %d: ID=%s, ChannelName=%s, Scheduled=%v",
			i+1, stream.ID, stream.ChannelName, stream.StartScheduled)

		if stream.ID == "" {
			t.Errorf("스트림 %d: ID가 비어있음", i+1)
		}
		if stream.ChannelName == "" {
			t.Errorf("스트림 %d: ChannelName이 비어있음", i+1)
		}
		if stream.Link == nil || *stream.Link == "" {
			t.Errorf("스트림 %d: Link가 비어있음", i+1)
		}
	}
}
