package botruntime

import (
	"log/slog"
)

func (r *BotRuntime) logInfo(msg string) {
	if r.Logger != nil {
		r.Logger.Info(msg)
	}
}

func (r *BotRuntime) logError(msg string, err error) {
	if r.Logger != nil {
		r.Logger.Error(msg, slog.Any("error", err))
	}
}
