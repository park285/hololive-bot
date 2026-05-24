package ingress

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
)

const EventCommandReceived = "bot.command.received"

func ingressAttrs(commandType, userID, userName, chatID, roomName, rawMessage string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("command_type", strings.TrimSpace(commandType)),
		slog.String("user_id", strings.TrimSpace(userID)),
		slog.String("user_name", strings.TrimSpace(userName)),
		slog.String("room_id", strings.TrimSpace(chatID)),
		slog.String("room_name", strings.TrimSpace(roomName)),
	}
	attrs = append(attrs, messageSummaryAttrs(rawMessage)...)
	return attrs
}

func messageSummaryAttrs(message string) []slog.Attr {
	message = strings.TrimSpace(message)
	if message == "" {
		return []slog.Attr{slog.Int("message_len", 0)}
	}
	sum := sha256.Sum256([]byte(message))
	return []slog.Attr{
		slog.Int("message_len", len(message)),
		slog.String("message_sha256_8", hex.EncodeToString(sum[:8])),
	}
}
