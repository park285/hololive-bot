package format

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type buildTemplateDataCase struct {
	name      string
	item      domain.YouTubeNotificationOutbox
	wantURL   string
	wantTitle string
	wantPost  string
	wantErr   bool
}

func TestBuildTemplateData(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	tests := []buildTemplateDataCase{
		{
			name: "video payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewVideo,
				Payload: `{"video_id":"vid1","title":"영상1"}`,
			},
			wantURL:   "https://youtu.be/vid1",
			wantTitle: "영상1",
		},
		{
			name: "short payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewShort,
				Payload: `{"video_id":"short1","title":"쇼츠1"}`,
			},
			wantURL:   "https://www.youtube.com/shorts/short1",
			wantTitle: "쇼츠1",
		},
		{
			name: "community payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindCommunityPost,
				Payload: `{"post_id":"post1","content_text":"내용"}`,
			},
			wantURL:  "https://www.youtube.com/post/post1",
			wantPost: "post1",
		},
		{
			name: "invalid payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewVideo,
				Payload: `{invalid-json}`,
			},
			wantErr: true,
		},
		{
			name: "unknown kind",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKind("UNKNOWN"),
				Payload: `{"video_id":"vid1","title":"영상1"}`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertBuildTemplateData(t, mf, tt)
		})
	}
}

func assertBuildTemplateData(t *testing.T, mf *MessageFormatter, tt buildTemplateDataCase) {
	t.Helper()

	got, err := mf.BuildTemplateData("멤버", &tt.item)
	if tt.wantErr {
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		return
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.MemberName != "멤버" || got.Kind != string(tt.item.Kind) || got.URL != tt.wantURL || got.Title != tt.wantTitle || got.PostID != tt.wantPost {
		t.Fatalf("unexpected template data: %#v", got)
	}
}

func TestBuildGroupedTemplateData(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	items := []domain.YouTubeNotificationOutbox{
		{Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		{Kind: domain.OutboxKindCommunityPost, Payload: `{"post_id":"p1","content_text":"내용"}`},
	}

	got, err := mf.BuildGroupedTemplateData("멤버", domain.OutboxKindNewVideo, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.MemberName != "멤버" || got.Kind != string(domain.OutboxKindNewVideo) || got.Count != 2 || len(got.Items) != 2 {
		t.Fatalf("unexpected grouped template header: %#v", got)
	}

	if got.Items[0].Title != "영상1" || got.Items[0].URL != "https://youtu.be/v1" {
		t.Fatalf("unexpected first item: %#v", got.Items[0])
	}

	if got.Items[1].ContentText != "내용" || got.Items[1].URL != "https://www.youtube.com/post/p1" {
		t.Fatalf("unexpected second item: %#v", got.Items[1])
	}
}

// 묶음 안의 항목 하나라도 읽지 못하면 빈 항목으로 보내지 않고 묶음 전체를 포맷 실패로 돌려준다.
func TestBuildGroupedTemplateDataRejectsUnreadableItems(t *testing.T) {
	t.Parallel()

	valid := domain.YouTubeNotificationOutbox{ID: 1, Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`}

	for _, tc := range []struct {
		name string
		item domain.YouTubeNotificationOutbox
	}{
		{name: "invalid video payload", item: domain.YouTubeNotificationOutbox{ID: 2, Kind: domain.OutboxKindNewShort, Payload: `{invalid}`}},
		{name: "invalid community payload", item: domain.YouTubeNotificationOutbox{ID: 2, Kind: domain.OutboxKindCommunityPost, Payload: `[]`}},
		{name: "unknown kind", item: domain.YouTubeNotificationOutbox{ID: 2, Kind: domain.OutboxKind("UNKNOWN"), Payload: `{}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := (&MessageFormatter{}).BuildGroupedTemplateData("멤버", domain.OutboxKindNewVideo, []domain.YouTubeNotificationOutbox{valid, tc.item})
			if err == nil {
				t.Fatal("expected grouped template data error")
			}
		})
	}
}

func TestFormatGroupedMessageErrors(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	if _, err := mf.FormatGroupedMessage(t.Context(), "멤버", "ch1", domain.OutboxKindNewVideo, nil); err == nil {
		t.Fatal("expected empty items error")
	}

	if _, err := mf.FormatGroupedMessage(t.Context(), "멤버", "ch1", domain.OutboxKind("UNKNOWN"), []domain.YouTubeNotificationOutbox{{}}); err == nil {
		t.Fatal("expected unsupported grouped kind error")
	}

	validItem := domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`}
	if _, err := mf.FormatGroupedMessage(t.Context(), "멤버", "ch1", domain.OutboxKindNewVideo, []domain.YouTubeNotificationOutbox{validItem}); err == nil {
		t.Fatal("expected nil renderer error")
	}
}
