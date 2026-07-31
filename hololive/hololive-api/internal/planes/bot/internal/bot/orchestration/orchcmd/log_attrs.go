package orchcmd

import (
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
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
		privacylog.RoomAttr(cmdCtx.Room, cmdCtx.RoomName),
		slog.String("user_id", strings.TrimSpace(cmdCtx.UserID)),
		slog.Bool("group_chat", cmdCtx.IsGroupChat),
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
