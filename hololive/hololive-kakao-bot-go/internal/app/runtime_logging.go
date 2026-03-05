package app

import (
	"log/slog"
)

func (r *BotRuntime) logInfo(msg string, attrs ...any) {
	if r.Logger != nil {
		r.Logger.Info(msg, attrs...)
	}
}

func (r *BotRuntime) logError(msg string, err error) {
	if r.Logger != nil {
		r.Logger.Error(msg, slog.Any("error", err))
	}
}
