package alarmdispatch

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRenderAlarmDispatchYouTubeOutboxRendersSSOT(t *testing.T) {
	t.Parallel()

	renderer := newAlarmDispatchTestRenderer(t)
	ctx := t.Context()

	singleEnvelope := domain.AlarmQueueEnvelope{YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1},
		Kind:       domain.OutboxKindNewShort,
		AlarmType:  domain.AlarmTypeShorts,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "short:abc", Payload: `{"video_id":"abc","title":"테스트 쇼츠"}`},
		},
	}}

	single, err := renderAlarmDispatchYouTubeOutbox(ctx, renderer, nil, &singleEnvelope)
	if err != nil {
		t.Fatalf("renderAlarmDispatchYouTubeOutbox(single) error = %v", err)
	}

	wantSingle := "🔔 멤버 새 쇼츠\n\u200b테스트 쇼츠\nhttps://www.youtube.com/shorts/abc"
	if single != wantSingle {
		t.Fatalf("single message = %q, want %q", single, wantSingle)
	}

	groupedEnvelope := domain.AlarmQueueEnvelope{YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1, 2},
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "post-a", Payload: `{"post_id":"post-a","content_text":"첫 글"}`},
			{OutboxID: 2, ContentID: "post-b", Payload: `{"post_id":"post-b","content_text":"둘째 글"}`},
		},
	}}

	grouped, err := renderAlarmDispatchYouTubeOutbox(ctx, renderer, nil, &groupedEnvelope)
	if err != nil {
		t.Fatalf("renderAlarmDispatchYouTubeOutbox(grouped) error = %v", err)
	}

	wantGrouped := "🔔 멤버 커뮤니티 글 · 2개\n\n1 · 첫 글\nhttps://www.youtube.com/post/post-a\n──────────\n2 · 둘째 글\nhttps://www.youtube.com/post/post-b"
	if grouped != wantGrouped {
		t.Fatalf("grouped message = %q, want %q", grouped, wantGrouped)
	}
}

func TestRenderAlarmDispatchYouTubeOutboxPreservesErrorContext(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1},
		Kind:       domain.OutboxKindNewShort,
		AlarmType:  domain.AlarmTypeShorts,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "short:abc", Payload: `{"video_id":"abc","title":"테스트 쇼츠"}`},
		},
	}}

	message, err := renderAlarmDispatchYouTubeOutbox(t.Context(), nil, nil, &envelope)
	want := fmt.Sprintf("format youtube outbox payload: format youtube outbox payload: format youtube outbox payload: render template: render template %s: renderer is nil", domain.TemplateKeyOutboxShorts)

	if message != "" || err == nil || err.Error() != want {
		t.Fatalf("renderAlarmDispatchYouTubeOutbox() = %q, %v; want empty message and %q", message, err, want)
	}
}

func TestRenderAlarmDispatchYouTubeOutboxRendersPremiereCountdown(t *testing.T) {
	t.Parallel()

	renderer := newAlarmDispatchTestRenderer(t)
	scheduled := time.Now().UTC().Add(30 * time.Minute)
	envelope := domain.AlarmQueueEnvelope{YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{2},
		Kind:       domain.OutboxKindNewVideo,
		AlarmType:  domain.AlarmTypeLive,
		ChannelID:  "UC_test",
		MemberName: "아크로라",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 2, ContentID: "premiere", Payload: `{"video_id":"premiere","title":"최초공개 영상","scheduled_start_at":"` + scheduled.Format(time.RFC3339Nano) + `","is_premiere":true}`},
		},
	}}

	premiere, err := renderAlarmDispatchYouTubeOutbox(t.Context(), renderer, nil, &envelope)
	if err != nil {
		t.Fatalf("renderAlarmDispatchYouTubeOutbox(premiere) error = %v", err)
	}

	if !strings.HasPrefix(premiere, "🔔 아크로라 30분 후 공개 예정\n") {
		t.Fatalf("premiere message = %q", premiere)
	}
}
