package api

import "log/slog"

func (h *MemberHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *AlarmHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *RoomHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *StreamHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *StatsHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *TemplateHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *MajorEventHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *AlarmHandler) logActivity(kind, summary string, details map[string]any) {
	if h != nil && h.activity != nil {
		h.activity.Log(kind, summary, details)
	}
}

func (h *RoomHandler) logActivity(kind, summary string, details map[string]any) {
	if h != nil && h.activity != nil {
		h.activity.Log(kind, summary, details)
	}
}
