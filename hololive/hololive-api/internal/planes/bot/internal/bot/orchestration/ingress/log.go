package ingress

import (
	"log/slog"
	"strings"
)

const EventCommandReceived = "bot.command.received"

func ingressAttrs(commandType, userID string, roomAttr slog.Attr, rawMessage string) []slog.Attr {
	summaryAttrs := messageSummaryAttrs(rawMessage)
	attrs := make([]slog.Attr, 0, 3+len(summaryAttrs))

	attrs = append(attrs,
		slog.String("command_type", strings.TrimSpace(commandType)),
		slog.String("user_id", strings.TrimSpace(userID)),
		roomAttr,
	)
	attrs = append(attrs, summaryAttrs...)

	return attrs
}

func messageSummaryAttrs(message string) []slog.Attr {
	return []slog.Attr{slog.Int("message_len", len(strings.TrimSpace(message)))}
}
