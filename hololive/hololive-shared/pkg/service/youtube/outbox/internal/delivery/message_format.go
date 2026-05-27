package delivery

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/format"
)

type DispatchPayloadFormatter = format.DispatchPayloadFormatter

func FormatYouTubeOutboxPayload(ctx context.Context, payload domain.YouTubeOutboxDispatchPayload) (string, error) {
	return format.FormatYouTubeOutboxPayload(ctx, payload)
}

func formatGroupedMessageFallback(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	return format.FormatGroupedMessageFallback(memberName, kind, items)
}
