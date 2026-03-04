package holodex

import (
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newTestMapper(t *testing.T) *StreamMapper {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewStreamMapper(logger)
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestMapStreamResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     *StreamRaw
		wantNil   bool
		checkFunc func(t *testing.T, s *domain.Stream)
	}{
		{
			name: "모든 필드가 있는 완전한 입력 - 유효한 Stream 반환",
			input: &StreamRaw{
				ID:             "vid-001",
				Title:          "테스트 방송",
				ChannelID:      strPtr("ch-001"),
				Status:         domain.StreamStatusLive,
				StartScheduled: strPtr("2024-01-01T10:00:00Z"),
				StartActual:    strPtr("2024-01-01T10:05:00Z"),
				Duration:       intPtr(3600),
				Thumbnail:      strPtr("https://example.com/thumb.jpg"),
				Link:           strPtr("https://youtube.com/watch?v=vid-001"),
				TopicID:        strPtr("gaming"),
				LiveViewers:    intPtr(1000),
				Channel: &ChannelRaw{
					ID:   "ch-001",
					Name: "Test Channel",
					Org:  strPtr("Hololive"),
				},
			},
			wantNil: false,
			checkFunc: func(t *testing.T, s *domain.Stream) {
				t.Helper()
				if s.ID != "vid-001" {
					t.Errorf("ID = %q, want %q", s.ID, "vid-001")
				}
				if s.Title != "테스트 방송" {
					t.Errorf("Title = %q, want %q", s.Title, "테스트 방송")
				}
				if s.ChannelID != "ch-001" {
					t.Errorf("ChannelID = %q, want %q", s.ChannelID, "ch-001")
				}
				if s.Status != domain.StreamStatusLive {
					t.Errorf("Status = %v, want %v", s.Status, domain.StreamStatusLive)
				}
				if s.Channel == nil {
					t.Error("Channel should not be nil")
				}
				if s.StartScheduled == nil {
					t.Error("StartScheduled should not be nil")
				}
				if s.StartActual == nil {
					t.Error("StartActual should not be nil")
				}
				if s.ViewerCount == nil || *s.ViewerCount != 1000 {
					t.Errorf("ViewerCount = %v, want 1000", s.ViewerCount)
				}
			},
		},
		{
			name: "ChannelID 없음 (Channel도 nil) - nil 반환",
			input: &StreamRaw{
				ID:    "vid-002",
				Title: "채널 없는 방송",
			},
			wantNil: true,
		},
		{
			name: "Channel이 nil이고 ChannelID가 있는 경우 - Channel 필드는 nil",
			input: &StreamRaw{
				ID:        "vid-003",
				Title:     "채널 nil 방송",
				ChannelID: strPtr("ch-003"),
				Channel:   nil,
			},
			wantNil: false,
			checkFunc: func(t *testing.T, s *domain.Stream) {
				t.Helper()
				if s.Channel != nil {
					t.Errorf("Channel = %v, want nil", s.Channel)
				}
				if s.ChannelID != "ch-003" {
					t.Errorf("ChannelID = %q, want %q", s.ChannelID, "ch-003")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapper := newTestMapper(t)
			got := mapper.MapStreamResponse(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("MapStreamResponse() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("MapStreamResponse() = nil, want non-nil")
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestMapChannelResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     *ChannelRaw
		checkFunc func(t *testing.T, c *domain.Channel)
	}{
		{
			name: "모든 필드가 있는 완전한 채널 입력 - 유효한 Channel 반환",
			input: &ChannelRaw{
				ID:          "ch-100",
				Name:        "하이라이트 채널",
				EnglishName: strPtr("Highlight Channel"),
				Photo:       strPtr("https://example.com/photo.jpg"),
				Twitter:     strPtr("@highlight"),
				VideoCount:  intPtr(200),
				Org:         strPtr("Hololive"),
				Suborg:      strPtr("HololiveJP"),
				Group:       strPtr("Gen3"),
			},
			checkFunc: func(t *testing.T, c *domain.Channel) {
				t.Helper()
				if c.ID != "ch-100" {
					t.Errorf("ID = %q, want %q", c.ID, "ch-100")
				}
				if c.Name != "하이라이트 채널" {
					t.Errorf("Name = %q, want %q", c.Name, "하이라이트 채널")
				}
				if c.EnglishName == nil || *c.EnglishName != "Highlight Channel" {
					t.Errorf("EnglishName = %v, want %q", c.EnglishName, "Highlight Channel")
				}
				if c.Org == nil || *c.Org != "Hololive" {
					t.Errorf("Org = %v, want %q", c.Org, "Hololive")
				}
				if c.Suborg == nil || *c.Suborg != "HololiveJP" {
					t.Errorf("Suborg = %v, want %q", c.Suborg, "HololiveJP")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapper := newTestMapper(t)
			got := mapper.MapChannelResponse(tt.input)
			if got == nil {
				t.Fatal("MapChannelResponse() = nil, want non-nil")
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestMapStreamsResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     []StreamRaw
		wantCount int
	}{
		{
			name:      "빈 입력 - 빈 슬라이스 반환",
			input:     []StreamRaw{},
			wantCount: 0,
		},
		{
			name: "여러 항목 - ChannelID 없는 항목은 필터링됨",
			input: []StreamRaw{
				// 유효한 스트림 (ChannelID 있음)
				{
					ID:        "vid-a",
					Title:     "방송 A",
					ChannelID: strPtr("ch-a"),
				},
				// ChannelID 없음 - nil 반환 → 필터링
				{
					ID:    "vid-b",
					Title: "채널 없는 방송 B",
				},
				// 유효한 스트림 (Channel.ID로 대체)
				{
					ID:    "vid-c",
					Title: "방송 C",
					Channel: &ChannelRaw{
						ID:   "ch-c",
						Name: "채널 C",
					},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapper := newTestMapper(t)
			got := mapper.MapStreamsResponse(tt.input)
			if len(got) != tt.wantCount {
				t.Errorf("MapStreamsResponse() count = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}
