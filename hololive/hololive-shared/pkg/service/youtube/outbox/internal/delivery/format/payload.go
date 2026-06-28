package format

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type DispatchPayloadFormatter interface {
	FormatYouTubeOutboxPayload(ctx context.Context, payload *domain.YouTubeOutboxDispatchPayload) (string, error)
}

func FormatYouTubeOutboxPayload(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	return (&MessageFormatter{Renderer: renderer, MessageStrings: messageStrings}).FormatYouTubeOutboxPayload(ctx, payload)
}

func (mf *MessageFormatter) FormatYouTubeOutboxPayload(ctx context.Context, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	if err := payload.Validate(); err != nil {
		return "", fmt.Errorf("format youtube outbox payload: %w", err)
	}
	if msg := strings.TrimSpace(payload.PreRenderedMessage); msg != "" {
		return msg, nil
	}

	memberName := strings.TrimSpace(payload.MemberName)
	if memberName == "" {
		memberName = mf.MessageStrings.VTuberFallbackContext(ctx)
	}

	items := notificationOutboxItemsFromDispatchPayload(payload)
	if len(items) == 1 {
		data, err := mf.BuildTemplateData(memberName, &items[0])
		if err != nil {
			return "", err
		}
		return mf.renderTemplate(ctx, items[0].Kind.ToTemplateKey(), items[0].ChannelID, data)
	}
	return mf.FormatGroupedMessage(ctx, memberName, payload.ChannelID, payload.Kind, items)
}

func notificationOutboxItemsFromDispatchPayload(payload *domain.YouTubeOutboxDispatchPayload) []domain.YouTubeNotificationOutbox {
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
