package server

import (
	"context"

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
)

type SettingsActivityLogger interface {
	Log(entryType, summary string, details map[string]any)
}

type SettingsReadRecentLogsFunc func(limit int) (any, error)

type SettingsApplier = sharedsettings.SettingsApplier

type ConfigPublisher interface {
	PublishScraperProxy(ctx context.Context, enabled bool) error
	PublishAlarmAdvanceMinutes(ctx context.Context, minutes int) error
}
