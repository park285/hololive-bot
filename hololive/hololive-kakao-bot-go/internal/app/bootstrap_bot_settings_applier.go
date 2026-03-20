// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	return a.base.ApplyScraperProxy(ctx, enabled)
}

func (a *botSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) sharedsettings.AlarmAdvanceMinutesApplyResult {
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
	return a.base.ScraperProxyRuntimeState(requested)
}
