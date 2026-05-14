package dispatch

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func groupAttrs(group NotificationGroup) []slog.Attr {
	return []slog.Attr{
		slog.String("room_id", group.RoomID),
		slog.Int("notifications", len(group.Notifications)),
		slog.Int("envelopes", len(group.Envelopes)),
	}
}

func envelopeBatchAttrs(envelopes []domain.AlarmQueueEnvelope) []slog.Attr {
	return []slog.Attr{
		slog.Int("envelopes", len(envelopes)),
	}
}
