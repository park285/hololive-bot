package app

import (
	"context"
	"log/slog"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type memberNewsWeeklyRunNowTrigger interface {
	SendMemberNewsWeekly(ctx context.Context) error
}

type botSettingsApplier struct {
	base             sharedserver.SettingsApplier
	memberNewsRunNow memberNewsWeeklyRunNowTrigger
	logger           *slog.Logger
}

func newBotSettingsApplier(
	base sharedserver.SettingsApplier,
	memberNewsRunNow memberNewsWeeklyRunNowTrigger,
	logger *slog.Logger,
) sharedserver.SettingsApplier {
	if logger == nil {
		logger = slog.Default()
	}

	return &botSettingsApplier{
		base:             base,
		memberNewsRunNow: memberNewsRunNow,
		logger:           logger,
	}
}

func (a *botSettingsApplier) ApplyScraperProxy(ctx context.Context, enabled bool) sharedserver.ScraperProxyApplyResult {
	if a.base == nil {
		applied := false
		return sharedserver.ScraperProxyApplyResult{
			Requested: enabled,
			Applied:   &applied,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ApplyScraperProxy(ctx, enabled)
}

func (a *botSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) sharedserver.AlarmAdvanceMinutesApplyResult {
	if a.base == nil {
		return sharedserver.AlarmAdvanceMinutesApplyResult{
			AlarmRequestedAdvanceMinutes: minutes,
			AlarmApplied:                 false,
			AlarmReason:                  "settings applier not configured",
		}
	}
	return a.base.ApplyAlarmAdvanceMinutes(ctx, minutes)
}

func (a *botSettingsApplier) ApplyMemberNewsWeeklyRunNow(ctx context.Context) sharedserver.MemberNewsWeeklyRunNowResult {
	if a.memberNewsRunNow == nil {
		return sharedserver.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger is not configured",
		}
	}

	if err := a.memberNewsRunNow.SendMemberNewsWeekly(ctx); err != nil {
		a.logger.Warn("Failed to trigger member news weekly run-now", slog.Any("error", err))
		return sharedserver.MemberNewsWeeklyRunNowResult{
			Applied: false,
			Reason:  "member news trigger failed",
			Error:   err.Error(),
		}
	}

	return sharedserver.MemberNewsWeeklyRunNowResult{
		Applied: true,
		Source:  "member_news_trigger",
	}
}

func (a *botSettingsApplier) ScraperProxyRuntimeState(requested bool) sharedserver.ScraperProxyRuntimeStateResult {
	if a.base == nil {
		known := false
		return sharedserver.ScraperProxyRuntimeStateResult{
			Requested: requested,
			Known:     &known,
			Reason:    "settings applier not configured",
		}
	}
	return a.base.ScraperProxyRuntimeState(requested)
}
