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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	alarmscheduler "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/scheduler"
)

func buildAlarmRuntimeScheduler(
	cfg *config.Config,
	infra *coreInfrastructure,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: config is nil")
	}
	if infra == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: infrastructure is nil")
	}
	if infra.deps == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: bot dependencies are nil")
	}

	if infra.deps.Cache == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: cache dependency is nil")
	}
	if infra.holodexService == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: holodex service is nil")
	}
	if infra.deps.Chzzk == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: chzzk client is nil")
	}
	if infra.deps.Twitch == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: twitch client is nil")
	}
	if infra.alarmService == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: alarm service is nil")
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		infra.deps.Cache,
		infra.holodexService,
		infra.deps.Chzzk,
		infra.deps.Twitch,
		infra.alarmService,
		cfg.Notification,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: %w", err)
	}

	return scheduler, nil
}
