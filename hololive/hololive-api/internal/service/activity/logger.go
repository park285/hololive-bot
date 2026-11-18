package activity

import "log/slog"

type Logger struct {
	logger *slog.Logger
}

func NewActivityLogger(logger *slog.Logger) *Logger {
	return &Logger{logger: logger}
}

func (l *Logger) Log(entryType, summary string, details []slog.Attr) {
	l.logger.Info("activity",
		slog.String("type", entryType),
		slog.String("summary", summary),
		slog.GroupAttrs("details", details...),
	)
}
