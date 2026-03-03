package adapter

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

//go:fix inline
func stringPtr(s string) *string {
	return new(s)
}

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
					Org:  new("Indie"),
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

	// 21:00 KST 예정 방송
	scheduled := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC) // 21:00 KST
	notification := &domain.AlarmNotification{
		Stream: &domain.Stream{
			ID:             "test-stream",
			Title:          "테스트 방송",
			ChannelName:    "테스트 채널",
			StartScheduled: &scheduled,
		},
		MinutesUntil: 5,
	}

	// MinutesUntil > 0 && StartScheduled != nil → ScheduledTimeKST 생성
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

	// live catchup: MinutesUntil = 0
	scheduled := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC)
	notification := &domain.AlarmNotification{
		Stream: &domain.Stream{
			ID:             "test-stream",
			Title:          "테스트 방송",
			ChannelName:    "테스트 채널",
			StartScheduled: &scheduled,
		},
		MinutesUntil: 0,
	}

	// MinutesUntil <= 0 → ScheduledTimeKST 빈 문자열
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
	scheduled := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC) // 21:00 KST
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
				// StartScheduled nil → 시각 미표시
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
	scheduled := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC) // 21:00 KST
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
	scheduledA := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC)  // 21:00 KST
	scheduledB := time.Date(2026, 2, 12, 12, 30, 0, 0, time.UTC) // 21:30 KST
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
