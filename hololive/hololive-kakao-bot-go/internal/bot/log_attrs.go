package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func commandExecutionAttrs(cmdCtx *domain.CommandContext, commandKey string, cmdType domain.CommandType) []slog.Attr {
	attrs := commandContextAttrs(cmdCtx, commandKey)
	attrs = append(attrs, slog.String("command_type", cmdType.String()))
	return attrs
}

func commandContextAttrs(cmdCtx *domain.CommandContext, commandKey string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("command", strings.TrimSpace(commandKey)),
	}
	if cmdCtx == nil {
		return attrs
	}

	attrs = append(attrs,
		slog.String("room_id", strings.TrimSpace(cmdCtx.Room)),
		slog.String("room_name", strings.TrimSpace(cmdCtx.RoomName)),
		slog.String("user_id", strings.TrimSpace(cmdCtx.UserID)),
		slog.String("user_name", strings.TrimSpace(cmdCtx.UserName)),
		slog.Bool("group_chat", cmdCtx.IsGroupChat),
	)
	if cmdCtx.ThreadID != nil && strings.TrimSpace(*cmdCtx.ThreadID) != "" {
		attrs = append(attrs, slog.String("thread_id", strings.TrimSpace(*cmdCtx.ThreadID)))
	}

	attrs = append(attrs, messageSummaryAttrs(cmdCtx.Message)...)
	return attrs
}

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
