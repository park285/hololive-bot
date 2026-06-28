package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestFormatYouTubeOutboxPayloadRendersSSOT(t *testing.T) {
	t.Parallel()

	db := newDeliveryPool(t)
	renderer := template.NewRenderer(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	single, err := FormatYouTubeOutboxPayload(ctx, renderer, nil, &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1},
		Kind:       domain.OutboxKindNewShort,
		AlarmType:  domain.AlarmTypeShorts,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "short:abc", Payload: `{"video_id":"abc","title":"테스트 쇼츠"}`},
		},
	})
	if err != nil {
		t.Fatalf("FormatYouTubeOutboxPayload(single) error = %v", err)
	}
	wantSingle := "📱 멤버 쇼츠 알림\n테스트 쇼츠\nhttps://www.youtube.com/shorts/abc"
	if single != wantSingle {
		t.Fatalf("single message = %q, want %q", single, wantSingle)
	}

	grouped, err := FormatYouTubeOutboxPayload(ctx, renderer, nil, &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1, 2},
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "post-a", Payload: `{"post_id":"post-a","content_text":"첫 글"}`},
			{OutboxID: 2, ContentID: "post-b", Payload: `{"post_id":"post-b","content_text":"둘째 글"}`},
		},
	})
	if err != nil {
		t.Fatalf("FormatYouTubeOutboxPayload(grouped) error = %v", err)
	}
	wantGrouped := "📝 멤버 커뮤니티 알림 (2개)\n1. 첫 글\n   https://www.youtube.com/post/post-a\n\n2. 둘째 글\n   https://www.youtube.com/post/post-b"
	if grouped != wantGrouped {
		t.Fatalf("grouped message = %q, want %q", grouped, wantGrouped)
	}

	if _, err := FormatYouTubeOutboxPayload(ctx, nil, nil, &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1},
		Kind:       domain.OutboxKindNewShort,
		AlarmType:  domain.AlarmTypeShorts,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "short:abc", Payload: `{"video_id":"abc","title":"테스트 쇼츠"}`},
		},
	}); err == nil {
		t.Fatalf("expected error when renderer is nil")
	}
}
