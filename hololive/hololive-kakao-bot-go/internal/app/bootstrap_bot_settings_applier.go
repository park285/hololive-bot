package app

import (
	"context"
	"log/slog"

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
)

type memberNewsWeeklyRunNowTrigger interface {
	SendMemberNewsWeekly(ctx context.Context) error
}

type botSettingsApplier struct {
	base             sharedsettings.SettingsApplier
	memberNewsRunNow memberNewsWeeklyRunNowTrigger
	logger           *slog.Logger
}

func newBotSettingsApplier(
	base sharedsettings.SettingsApplier,
	memberNewsRunNow memberNewsWeeklyRunNowTrigger,
	logger *slog.Logger,
) sharedsettings.SettingsApplier {
	if logger == nil {
		logger = slog.Default()
	}

	return &botSettingsApplier{
		base:             base,
		memberNewsRunNow: memberNewsRunNow,
		logger:           logger,
	}
}

func (a *botSettingsApplier) ApplyScraperProxy(ctx context.Context, enabled bool) sharedsettings.ScraperProxyApplyResult {
	if a.base == nil {
		applied := false
		return sharedsettings.ScraperProxyApplyResult{
			Requested: enabled,
			Applied:   &applied,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ApplyScraperProxy(ctx, enabled)
}

func (a *botSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) sharedsettings.AlarmAdvanceMinutesApplyResult {
	if a.base == nil {
		return sharedsettings.AlarmAdvanceMinutesApplyResult{
			AlarmRequestedAdvanceMinutes: minutes,
			AlarmApplied:                 false,
			AlarmReason:                  "settings applier not configured",
		}
	}
	return a.base.ApplyAlarmAdvanceMinutes(ctx, minutes)
}

func (a *botSettingsApplier) ApplyMemberNewsWeeklyRunNow(ctx context.Context) sharedsettings.MemberNewsWeeklyRunNowResult {
	if a.memberNewsRunNow == nil {
		return sharedsettings.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger is not configured",
		}
	}

	if err := a.memberNewsRunNow.SendMemberNewsWeekly(ctx); err != nil {
		a.logger.Warn("Failed to trigger member news weekly run-now", slog.Any("error", err))
		return sharedsettings.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger failed",
			Error:   err.Error(),
		}
	}

	return sharedsettings.MemberNewsWeeklyRunNowResult{
		Applied: true,
		Source:  "member_news_trigger",
	}
}

func (a *botSettingsApplier) ScraperProxyRuntimeState(requested bool) sharedsettings.ScraperProxyRuntimeStateResult {
	if a.base == nil {
		known := false
		return sharedsettings.ScraperProxyRuntimeStateResult{
			Requested: requested,
			Known:     &known,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ScraperProxyRuntimeState(requested)
}
