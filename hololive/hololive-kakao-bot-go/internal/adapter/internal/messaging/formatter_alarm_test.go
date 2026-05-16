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

package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

func TestAlarmChannelName_WithStelliveOrg(t *testing.T) {
	tests := []struct {
		name         string
		notification *domain.AlarmNotification
		want         string
	}{
		{
			name: "Stellive member shows tag",
			notification: &domain.AlarmNotification{
				Channel: &domain.Channel{
					Name: "아야츠노 유니",
					Org:  new("Stellive"),
				},
			},
			want: "[스텔라이브] 아야츠노 유니",
		},
		{
			name: "Hololive member no tag",
			notification: &domain.AlarmNotification{
				Channel: &domain.Channel{
					Name: "사쿠라 미코",
					Org:  new("Hololive"),
				},
			},
			want: "사쿠라 미코",
		},
		{
			name: "Nijisanji member shows tag",
			notification: &domain.AlarmNotification{
				Channel: &domain.Channel{
					Name: "쿠제 혼지",
					Org:  new("Nijisanji"),
				},
			},
			want: "[니지산지] 쿠제 혼지",
		},
		{
			name: "VSPO member shows tag",
			notification: &domain.AlarmNotification{
				Channel: &domain.Channel{
					Name: "아카사키 치호",
					Org:  new("VSPO"),
				},
			},
			want: "[VSPO] 아카사키 치호",
		},
		{
			name: "Indie member shows tag",
			notification: &domain.AlarmNotification{
				Channel: &domain.Channel{
					Name: "유우키 사쿠나",
					Org:  new("Independents"),
				},
			},
			want: "[개인세] 유우키 사쿠나",
		},
		{
			name: "nil channel returns empty",
			notification: &domain.AlarmNotification{
				Stream: &domain.Stream{
					ChannelName: "Fallback Name",
				},
			},
			want: "Fallback Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := alarmChannelName(tt.notification)
			if got != tt.want {
				t.Errorf("alarmChannelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAlarmNotification_IntegratedURLs(t *testing.T) {
	tests := []struct {
		name            string
		stream          *domain.Stream
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "Integrated broadcast (YouTube + Chzzk)",
			stream: &domain.Stream{
				ID:             "abc123",
				Title:          "테스트 방송",
				ChannelName:    "아야츠노 유니",
				ChzzkChannelID: "f997979606554ef4827038e244845582",
				ChzzkLiveURL:   "https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
				IsIntegrated:   true,
			},
			wantContains: []string{
				"📺 YouTube:",
				"https://youtube.com/watch?v=abc123",
				"📺 치지직:",
				"https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
			},
		},
		{
			name: "Chzzk only broadcast",
			stream: &domain.Stream{
				Title:          "치지직 전용 방송",
				ChannelName:    "아야츠노 유니",
				ChzzkChannelID: "f997979606554ef4827038e244845582",
				ChzzkLiveURL:   "https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
				IsChzzkOnly:    true,
			},
			wantContains: []string{
				"📺 치지직:",
				"https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
			},
			wantNotContains: []string{
				"YouTube:",
			},
		},
		{
			name: "YouTube only broadcast (no Chzzk info)",
			stream: &domain.Stream{
				ID:          "xyz789",
				Title:       "YouTube 전용 방송",
				ChannelName: "사쿠라 미코",
			},
			wantContains: []string{
				"https://youtube.com/watch?v=xyz789",
			},
			wantNotContains: []string{
				"치지직:",
			},
		},
		{
			name: "Chzzk info present but no YouTube ID",
			stream: &domain.Stream{
				Title:          "치지직만",
				ChannelName:    "테스트",
				ChzzkChannelID: "f997979606554ef4827038e244845582",
				ChzzkLiveURL:   "https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
			},
			wantContains: []string{
				"📺 치지직:",
				"https://chzzk.naver.com/live/f997979606554ef4827038e244845582",
			},
			wantNotContains: []string{
				"YouTube:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notification := &domain.AlarmNotification{
				Stream:       tt.stream,
				MinutesUntil: 5,
			}

			var urlText string

			switch {
			case tt.stream.IsIntegrated && tt.stream.HasYouTubeInfo() && tt.stream.ChzzkChannelID != "":
				urlText = "📺 YouTube: " + tt.stream.GetYouTubeURL() + "\n📺 치지직: " + tt.stream.GetChzzkLiveURL()
			case tt.stream.IsChzzkOnly || (!tt.stream.HasYouTubeInfo() && tt.stream.ChzzkChannelID != ""):
				urlText = "📺 치지직: " + tt.stream.GetChzzkLiveURL()
			default:
				urlText = tt.stream.GetYouTubeURL()
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(urlText, want) {
					t.Errorf("URL text missing expected string %q\nGot: %s", want, urlText)
				}
			}

			for _, notWant := range tt.wantNotContains {
				if strings.Contains(urlText, notWant) {
					t.Errorf("URL text contains unexpected string %q\nGot: %s", notWant, urlText)
				}
			}

			_ = notification
		})
	}
}

func TestAlarmNotification_UpcomingScheduledTime(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
	notification := &domain.AlarmNotification{
		Stream: &domain.Stream{
			ID:             "test-stream",
			Title:          "테스트 방송",
			ChannelName:    "테스트 채널",
			StartScheduled: &scheduled,
		},
		MinutesUntil: 5,
	}

	var scheduledTimeKST string

	if notification.MinutesUntil > 0 && notification.Stream.StartScheduled != nil {
		scheduledTimeKST = util.FormatKST(*notification.Stream.StartScheduled, "15:04")
	}

	if scheduledTimeKST == "" {
		t.Fatal("expected ScheduledTimeKST to be set for upcoming notification")
	}

	if scheduledTimeKST != "21:00" {
		t.Errorf("expected ScheduledTimeKST = %q, got %q", "21:00", scheduledTimeKST)
	}
}

func TestAlarmNotification_LiveFallback(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
	notification := &domain.AlarmNotification{
		Stream: &domain.Stream{
			ID:             "test-stream",
			Title:          "테스트 방송",
			ChannelName:    "테스트 채널",
			StartScheduled: &scheduled,
		},
		MinutesUntil: 0,
	}

	var scheduledTimeKST string

	if notification.MinutesUntil > 0 && notification.Stream.StartScheduled != nil {
		scheduledTimeKST = util.FormatKST(*notification.Stream.StartScheduled, "15:04")
	}

	if scheduledTimeKST != "" {
		t.Errorf("expected empty ScheduledTimeKST for live catchup, got %q", scheduledTimeKST)
	}
}

func TestAlarmNotificationGroup_WithScheduledTime(t *testing.T) {
	t.Parallel()

	formatter := &ResponseFormatter{}
	scheduled := time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
	notifications := []*domain.AlarmNotification{
		{
			Channel: &domain.Channel{Name: "채널A"},
			Stream: &domain.Stream{
				ID:             "stream-a",
				Title:          "방송 A",
				ChannelName:    "채널A",
				StartScheduled: &scheduled,
			},
			MinutesUntil: 5,
		},
		{
			Channel: &domain.Channel{Name: "채널B"},
			Stream: &domain.Stream{
				ID:          "stream-b",
				Title:       "방송 B",
				ChannelName: "채널B",
			},
			MinutesUntil: 5,
		},
	}

	got := formatter.AlarmNotificationGroup(5, notifications)
	if !strings.Contains(got, "⏰ 21:00 방송예정") {
		t.Fatalf("expected absolute scheduled time in group header, got:\n%s", got)
	}

	if !strings.Contains(got, "1. 채널A (21:00 방송예정)") {
		t.Fatalf("expected absolute scheduled upcoming label for first entry, got:\n%s", got)
	}

	if !strings.Contains(got, "2. 채널B (방송예정)") {
		t.Fatalf("expected upcoming fallback label for second entry, got:\n%s", got)
	}
}

func TestAlarmNotificationGroup_LiveStartedLabel(t *testing.T) {
	t.Parallel()

	formatter := &ResponseFormatter{}
	scheduled := time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
	notifications := []*domain.AlarmNotification{
		{
			Channel: &domain.Channel{Name: "채널A"},
			Stream: &domain.Stream{
				ID:             "stream-a",
				Title:          "방송 A",
				ChannelName:    "채널A",
				StartScheduled: &scheduled,
			},
			MinutesUntil: 0,
		},
	}

	got := formatter.AlarmNotificationGroup(0, notifications)
	if !strings.Contains(got, "⏰ 여러 방송이 시작되었습니다.") {
		t.Fatalf("expected live started summary in group header, got:\n%s", got)
	}

	if !strings.Contains(got, "1. 채널A (21:00 방송 시작)") {
		t.Fatalf("expected live started label in group entry, got:\n%s", got)
	}
}

func TestAlarmNotificationGroup_HeaderWithMultipleScheduledTimes(t *testing.T) {
	t.Parallel()

	formatter := &ResponseFormatter{}
	scheduledA := time.Date(2026, time.February, 12, 12, 0, 0, 0, time.UTC)
	scheduledB := time.Date(2026, time.February, 12, 12, 30, 0, 0, time.UTC)
	notifications := []*domain.AlarmNotification{
		{
			Channel: &domain.Channel{Name: "채널A"},
			Stream: &domain.Stream{
				ID:             "stream-a",
				Title:          "방송 A",
				ChannelName:    "채널A",
				StartScheduled: &scheduledA,
			},
			MinutesUntil: 5,
		},
		{
			Channel: &domain.Channel{Name: "채널B"},
			Stream: &domain.Stream{
				ID:             "stream-b",
				Title:          "방송 B",
				ChannelName:    "채널B",
				StartScheduled: &scheduledB,
			},
			MinutesUntil: 5,
		},
	}

	got := formatter.AlarmNotificationGroup(5, notifications)
	if !strings.Contains(got, "⏰ 방송예정: 21:00, 21:30") {
		t.Fatalf("expected multi-time summary in group header, got:\n%s", got)
	}
}
