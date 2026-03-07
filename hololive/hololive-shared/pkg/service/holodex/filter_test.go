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

package holodex

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newTestFilter(t *testing.T) *StreamFilter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewStreamFilter(logger)
}

func TestFilterHololiveStreams(t *testing.T) {
	t.Parallel()

	hololiveOrg := "Hololive"
	otherOrg := "Other"
	holostarsSuborg := "HOLOSTARS"

	tests := []struct {
		name      string
		input     []*domain.Stream
		wantCount int
		wantIDs   []string
	}{
		{
			name: "Channel nil인 스트림 - 필터링됨",
			input: []*domain.Stream{
				{ID: "s1", Channel: nil},
			},
			wantCount: 0,
		},
		{
			name: "허용되지 않는 org - 필터링됨",
			input: []*domain.Stream{
				{
					ID: "s2",
					Channel: &domain.Channel{
						ID:  "ch-2",
						Org: &otherOrg,
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "HOLOSTARS 서브org - 필터링됨",
			input: []*domain.Stream{
				{
					ID: "s3",
					Channel: &domain.Channel{
						ID:     "ch-3",
						Org:    &hololiveOrg,
						Suborg: &holostarsSuborg,
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "유효한 Hololive 채널 - 유지됨",
			input: []*domain.Stream{
				{
					ID:          "s4",
					ChannelName: "아쿠아",
					Channel: &domain.Channel{
						ID:  "ch-4",
						Org: &hololiveOrg,
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"s4"},
		},
		{
			name: "혼합 입력 - 유효한 스트림만 유지됨",
			input: []*domain.Stream{
				{ID: "s5", Channel: nil},
				{
					ID: "s6",
					Channel: &domain.Channel{
						ID:  "ch-6",
						Org: &hololiveOrg,
					},
				},
				{
					ID: "s7",
					Channel: &domain.Channel{
						ID:     "ch-7",
						Org:    &hololiveOrg,
						Suborg: &holostarsSuborg,
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"s6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newTestFilter(t)
			got := f.FilterHololiveStreams(tt.input)
			if len(got) != tt.wantCount {
				t.Errorf("FilterHololiveStreams() count = %d, want %d", len(got), tt.wantCount)
			}
			for i, wantID := range tt.wantIDs {
				if i >= len(got) {
					t.Errorf("결과 인덱스 %d 없음, want ID %q", i, wantID)
					continue
				}
				if got[i].ID != wantID {
					t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, wantID)
				}
			}
		})
	}
}

func TestFilterUpcomingStreams(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	tests := []struct {
		name      string
		input     []*domain.Stream
		wantCount int
	}{
		{
			name: "과거 시작 시각 - 필터링됨",
			input: []*domain.Stream{
				{ID: "s1", StartScheduled: &past},
			},
			wantCount: 0,
		},
		{
			name: "미래 시작 시각 - 유지됨",
			input: []*domain.Stream{
				{ID: "s2", StartScheduled: &future},
			},
			wantCount: 1,
		},
		{
			name: "StartScheduled nil - 유지됨",
			input: []*domain.Stream{
				{ID: "s3", StartScheduled: nil},
			},
			wantCount: 1,
		},
		{
			name: "StartActual이 있는 스트림 - 필터링됨 (이미 시작됨)",
			input: []*domain.Stream{
				{ID: "s4", StartScheduled: &future, StartActual: &past},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newTestFilter(t)
			got := f.FilterUpcomingStreams(tt.input)
			if len(got) != tt.wantCount {
				t.Errorf("FilterUpcomingStreams() count = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestIsHolostarsChannel(t *testing.T) {
	t.Parallel()

	holostarsOrg := "HOLOSTARS"
	holostarsSuborg := "HOLOSTARS"
	hololiveOrg := "Hololive"

	tests := []struct {
		name    string
		channel *domain.Channel
		want    bool
	}{
		{
			name: "Suborg이 HOLOSTARS - true",
			channel: &domain.Channel{
				ID:     "ch-1",
				Name:   "SomeChannel",
				Suborg: &holostarsSuborg,
			},
			want: true,
		},
		{
			name: "Org이 Hololive, Suborg 없음 - false",
			channel: &domain.Channel{
				ID:  "ch-2",
				Org: &hololiveOrg,
			},
			want: false,
		},
		{
			name:    "nil 채널 - false",
			channel: nil,
			want:    false,
		},
		{
			name: "Name에 HOLOSTARS 포함 - true",
			channel: &domain.Channel{
				ID:  "ch-3",
				Org: &holostarsOrg,
			},
			want: false, // Org은 체크하지 않음, Name/Suborg/EnglishName만 체크
		},
		{
			name: "영문 이름에 HOLOSTARS 포함 - true",
			channel: &domain.Channel{
				ID:          "ch-4",
				EnglishName: strPtr("HOLOSTARS English"),
			},
			want: true,
		},
		{
			name: "채널 이름에 holostars(소문자) 포함 - true (대소문자 무관)",
			channel: &domain.Channel{
				ID:   "ch-5",
				Name: "holostars channel",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newTestFilter(t)
			got := f.IsHolostarsChannel(tt.channel)
			if got != tt.want {
				t.Errorf("IsHolostarsChannel() = %v, want %v", got, tt.want)
			}
		})
	}
}
