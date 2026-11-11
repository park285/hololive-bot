package orchcmd

import (
	"log/slog"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

func commandExecutionAttrs(cmdCtx *botcommand.CommandContext, commandKey string, cmdType botcommand.CommandType) []slog.Attr {
	attrs := commandContextAttrs(cmdCtx, commandKey)

	attrs = append(attrs, slog.String("command_type", cmdType.String()))

	return attrs
}

func commandContextAttrs(cmdCtx *botcommand.CommandContext, commandKey string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("command", strings.TrimSpace(commandKey)),
	}

	if cmdCtx == nil {
		return attrs
	}

	attrs = append(attrs,
		sharedprivacylog.RoomIDAttr(cmdCtx.Room),
		slog.String("user_id", strings.TrimSpace(cmdCtx.UserID)),
	)
	if cmdCtx.ThreadID != nil && strings.TrimSpace(*cmdCtx.ThreadID) != "" {
		attrs = append(attrs, slog.String("thread_id", strings.TrimSpace(*cmdCtx.ThreadID)))
	}

	attrs = append(attrs, messageSummaryAttrs(cmdCtx.Message)...)

	return attrs
}

func messageSummaryAttrs(message string) []slog.Attr {
	return []slog.Attr{slog.Int("message_len", len(strings.TrimSpace(message)))}
}
