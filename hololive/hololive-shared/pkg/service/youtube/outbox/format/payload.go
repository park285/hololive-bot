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

// FormatYouTubeOutboxPayload는 dispatch payload를 최종 알림 본문으로 렌더링한다. 접기는 여러 항목 묶음에만 적용된다.
func FormatYouTubeOutboxPayload(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, seeMoreFold bool, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	out, err := (&MessageFormatter{Renderer: renderer, MessageStrings: messageStrings, SeeMoreFold: seeMoreFold}).FormatYouTubeOutboxPayload(ctx, payload)
	if err != nil {
		return out, fmt.Errorf("format youtube outbox payload: %w", err)
	}

	return out, nil
}

func (mf *MessageFormatter) FormatYouTubeOutboxPayload(ctx context.Context, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	if err := payload.Validate(); err != nil {
		return "", fmt.Errorf("format youtube outbox payload: %w", err)
	}

	memberName := strings.TrimSpace(payload.MemberName)
	if memberName == "" {
		memberName = mf.MessageStrings.Text(messagestrings.MiscVTuberFallback)
	}

	items := notificationOutboxItemsFromDispatchPayload(payload)
	if len(items) == 1 {
		out, itemErr := mf.formatSingleOutboxItem(ctx, memberName, &items[0])

		return out, itemErr
	}

	out, err := mf.FormatGroupedMessage(ctx, memberName, payload.ChannelID, payload.Kind, items)
	if err != nil {
		return out, fmt.Errorf("format grouped message: %w", err)
	}

	return out, nil
}

func (mf *MessageFormatter) formatSingleOutboxItem(ctx context.Context, memberName string, item *domain.YouTubeNotificationOutbox) (string, error) {
	data, err := mf.BuildTemplateData(memberName, item)
	if err != nil {
		return "", fmt.Errorf("build template data: %w", err)
	}

	out, err := mf.renderTemplate(ctx, item.Kind.ToTemplateKey(), item.ChannelID, data)
	if err != nil {
		return out, fmt.Errorf("render template: %w", err)
	}

	return out, nil
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
