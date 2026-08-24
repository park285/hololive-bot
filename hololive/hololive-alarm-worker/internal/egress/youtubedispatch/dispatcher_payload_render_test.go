package youtubedispatch

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestFormatYouTubeOutboxPayloadRendersSSOT(t *testing.T) {
	t.Parallel()

	db := newDeliveryPool(t)
	renderer := template.NewRenderer(db, slog.New(slog.DiscardHandler))
	ctx := t.Context()

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

	wantSingle := "🔔 **멤버** 새 쇼츠\n[테스트 쇼츠](https://www.youtube.com/shorts/abc)"
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

	wantGrouped := "## 🔔 멤버 커뮤니티 글 (2)\n1. 첫 글\n   [커뮤니티 글 보기](https://www.youtube.com/post/post-a)\n2. 둘째 글\n   [커뮤니티 글 보기](https://www.youtube.com/post/post-b)"
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
		t.Fatal("expected error when renderer is nil")
	}
}

func TestFormatYouTubeOutboxPayloadRendersPremiereCountdown(t *testing.T) {
	t.Parallel()

	db := newDeliveryPool(t)
	renderer := template.NewRenderer(db, slog.New(slog.DiscardHandler))
	scheduled := time.Now().UTC().Add(30 * time.Minute)

	premiere, err := FormatYouTubeOutboxPayload(t.Context(), renderer, nil, &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{2},
		Kind:       domain.OutboxKindNewVideo,
		AlarmType:  domain.AlarmTypeLive,
		ChannelID:  "UC_test",
		MemberName: "아크로라",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 2, ContentID: "premiere", Payload: `{"video_id":"premiere","title":"최초공개 영상","scheduled_start_at":"` + scheduled.Format(time.RFC3339Nano) + `","is_premiere":true}`},
		},
	})
	if err != nil {
		t.Fatalf("FormatYouTubeOutboxPayload(premiere) error = %v", err)
	}

	if !strings.HasPrefix(premiere, "🔔 **아크로라** 30분 후 공개 예정\n") {
		t.Fatalf("premiere message = %q", premiere)
	}
}
