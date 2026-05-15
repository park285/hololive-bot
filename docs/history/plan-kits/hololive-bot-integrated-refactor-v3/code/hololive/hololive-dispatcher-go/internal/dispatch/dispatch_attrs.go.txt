package dispatch

import (
	"log/slog"
)

func groupAttrs(group NotificationGroup) []slog.Attr {
	return []slog.Attr{
		slog.String("room_id", group.RoomID),
		slog.Int("notifications", len(group.Notifications)),
		slog.Int("envelopes", len(group.Envelopes)),
	}
}
