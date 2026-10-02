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
		{Kind: domain.OutboxKindNewShort, Payload: `{invalid}`},
		{Kind: domain.OutboxKindCommunityPost, Payload: `{"post_id":"p1","content_text":"내용"}`},
	}

	got := mf.BuildGroupedTemplateData("멤버", domain.OutboxKindNewVideo, items)
	if got.MemberName != "멤버" || got.Kind != string(domain.OutboxKindNewVideo) || got.Count != 3 || len(got.Items) != 3 {
		t.Fatalf("unexpected grouped template header: %#v", got)
	}

	if got.Items[0].Title != "영상1" || got.Items[0].URL != "https://youtu.be/v1" {
		t.Fatalf("unexpected first item: %#v", got.Items[0])
	}

	if got.Items[1].Title != "" || got.Items[1].URL != "" {
		t.Fatalf("expected invalid payload item to stay empty: %#v", got.Items[1])
	}

	if got.Items[2].ContentText != "내용" || got.Items[2].URL != "https://www.youtube.com/post/p1" {
		t.Fatalf("unexpected third item: %#v", got.Items[2])
	}
}

func TestFormatGroupedMessageErrors(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	if _, err := mf.FormatGroupedMessage(t.Context(), "멤버", "ch1", domain.OutboxKindNewVideo, nil); err == nil {
		t.Fatal("expected empty items error")
	}

	if _, err := mf.FormatGroupedMessage(t.Context(), "멤버", "ch1", domain.OutboxKindNewVideo, []domain.YouTubeNotificationOutbox{{}}); err == nil {
		t.Fatal("expected nil renderer error")
	}
}
