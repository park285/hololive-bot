package outbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

type DispatchPayloadFormatter interface {
	FormatYouTubeOutboxPayload(ctx context.Context, payload domain.YouTubeOutboxDispatchPayload) (string, error)
}

func FormatYouTubeOutboxPayload(ctx context.Context, payload domain.YouTubeOutboxDispatchPayload) (string, error) {
	return (&MessageFormatter{}).FormatYouTubeOutboxPayload(ctx, payload)
}

func (mf *MessageFormatter) FormatYouTubeOutboxPayload(ctx context.Context, payload domain.YouTubeOutboxDispatchPayload) (string, error) {
	if err := payload.Validate(); err != nil {
		return "", fmt.Errorf("format youtube outbox payload: %w", err)
	}
	if msg := strings.TrimSpace(payload.PreRenderedMessage); msg != "" {
		return msg, nil
	}

	memberName := strings.TrimSpace(payload.MemberName)
	if memberName == "" {
		memberName = "VTuber"
	}

	items := notificationOutboxItemsFromDispatchPayload(payload)
	if len(items) == 1 {
		return mf.formatMessageFallback(memberName, items[0])
	}
	if mf != nil && mf.renderer != nil {
		return mf.formatGroupedMessage(ctx, memberName, payload.ChannelID, payload.Kind, items)
	}
	return formatGroupedMessageFallback(memberName, payload.Kind, items)
}

func notificationOutboxItemsFromDispatchPayload(payload domain.YouTubeOutboxDispatchPayload) []domain.YouTubeNotificationOutbox {
	items := make([]domain.YouTubeNotificationOutbox, 0, len(payload.Items))
	for i := range payload.Items {
		items = append(items, domain.YouTubeNotificationOutbox{
			ID:        payload.Items[i].OutboxID,
			Kind:      payload.Kind,
			ChannelID: payload.ChannelID,
			ContentID: payload.Items[i].ContentID,
			Payload:   payload.Items[i].Payload,
		})
	}
	return items
}

func formatGroupedMessageFallback(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	mf := &MessageFormatter{}
	_, header := mf.getGroupedTemplateKeyAndHeader(memberName, kind, len(items))
	renderedBody := renderGroupedFallbackBody(kind, items)
	if renderedBody == "" {
		return "", fmt.Errorf("format grouped youtube outbox fallback: rendered body is empty")
	}
	if kind == domain.OutboxKindCommunityPost {
		return util.ApplyKakaoSeeMorePadding(renderedBody, header), nil
	}
	return strings.TrimSpace(header + "\n" + renderedBody), nil
}

func renderGroupedFallbackBody(kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) string {
	var body strings.Builder
	for i := range items {
		if i > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(renderGroupedFallbackItem(kind, items[i]))
	}
	return strings.TrimSpace(body.String())
}

func renderGroupedFallbackItem(kind domain.OutboxKind, notification domain.YouTubeNotificationOutbox) string {
	item := buildGroupedItemData(notification)
	text := strings.TrimSpace(item.Title)
	if kind == domain.OutboxKindCommunityPost {
		text = strings.TrimSpace(item.ContentText)
	}
	if url := strings.TrimSpace(item.URL); url != "" {
		return strings.TrimSpace(text + "\n" + url)
	}
	return text
}
