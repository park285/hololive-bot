// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package holodexprovider

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// 이 테스트는 -tags=integration 플래그로만 실행됩니다.
func TestScraperLiveIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &ScraperService{
		httpClient: &http.Client{
			Timeout: constants.OfficialScheduleConfig.Timeout,
		},
		logger:        logger,
		baseURL:       constants.OfficialScheduleConfig.BaseURL,
		memberNameMap: make(map[string]string),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	streams, err := service.fetchAllStreams(ctx)
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
