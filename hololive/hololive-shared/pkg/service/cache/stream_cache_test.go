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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSetStreamsAndGetStreams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		key       string
		streams   []*domain.Stream
		ttl       time.Duration
		wantFound bool
		wantCount int
	}{
		{
			name: "스트림 저장 후 조회 - 정확히 일치",
			key:  "streams:live",
			streams: []*domain.Stream{
				{ID: "vid-001", Title: "방송 A", Status: domain.StreamStatusLive},
				{ID: "vid-002", Title: "방송 B", Status: domain.StreamStatusUpcoming},
			},
			ttl:       time.Minute,
			wantFound: true,
			wantCount: 2,
		},
		{
			// JSON []로 저장되고 언마샬 시 비어있지 않은 슬라이스로 복원되어 found=true
			name:      "빈 스트림 슬라이스 저장 후 조회 - 빈 슬라이스 반환",
			key:       "streams:empty",
			streams:   []*domain.Stream{},
			ttl:       time.Minute,
			wantFound: true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service, _ := newTestCacheService(t)
			ctx := context.Background()

			service.SetStreams(ctx, tt.key, tt.streams, tt.ttl)

			got, found := service.GetStreams(ctx, tt.key)
			if found != tt.wantFound {
				t.Errorf("GetStreams() found = %v, want %v", found, tt.wantFound)
			}
			if found && len(got) != tt.wantCount {
				t.Errorf("GetStreams() count = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestGetStreams_NonExistentKey(t *testing.T) {
	t.Parallel()

	service, _ := newTestCacheService(t)
	ctx := context.Background()

	got, found := service.GetStreams(ctx, "streams:nonexistent")
	if found {
		t.Errorf("GetStreams() found = true, want false for non-existent key")
	}
	if got != nil {
		t.Errorf("GetStreams() = %v, want nil for non-existent key", got)
	}
}

func TestSetStreams_TTLIsApplied(t *testing.T) {
	t.Parallel()

	service, mini := newTestCacheService(t)
	ctx := context.Background()

	streams := []*domain.Stream{
		{ID: "vid-ttl", Title: "TTL 테스트 방송"},
	}
	service.SetStreams(ctx, "streams:ttl-test", streams, time.Second)

	// TTL 만료 전 조회 - 성공
	_, found := service.GetStreams(ctx, "streams:ttl-test")
	if !found {
		t.Fatal("TTL 만료 전 GetStreams()에서 스트림을 찾지 못함")
	}

	// miniredis 시간 빠르게 진행하여 TTL 만료
	mini.FastForward(2 * time.Second)

	// TTL 만료 후 조회 - 실패
	_, found = service.GetStreams(ctx, "streams:ttl-test")
	if found {
		t.Error("TTL 만료 후에도 GetStreams()에서 스트림을 찾음 (기대: false)")
	}
}

func TestSetStreams_ContentMatch(t *testing.T) {
	t.Parallel()

	service, _ := newTestCacheService(t)
	ctx := context.Background()

	channelName := "테스트 채널"
	streams := []*domain.Stream{
		{
			ID:          "vid-match",
			Title:       "내용 일치 테스트",
			ChannelID:   "ch-match",
			ChannelName: channelName,
			Status:      domain.StreamStatusLive,
		},
	}
	service.SetStreams(ctx, "streams:match", streams, time.Minute)

	got, found := service.GetStreams(ctx, "streams:match")
	if !found {
		t.Fatal("GetStreams() found = false, want true")
	}
	if len(got) != 1 {
		t.Fatalf("GetStreams() count = %d, want 1", len(got))
	}

	s := got[0]
	if s.ID != "vid-match" {
		t.Errorf("ID = %q, want %q", s.ID, "vid-match")
	}
	if s.ChannelName != channelName {
		t.Errorf("ChannelName = %q, want %q", s.ChannelName, channelName)
	}
	if s.Status != domain.StreamStatusLive {
		t.Errorf("Status = %v, want %v", s.Status, domain.StreamStatusLive)
	}
}
