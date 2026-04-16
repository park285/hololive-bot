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
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

const (
	notificationSchedulerRoleEnv = "NOTIFICATION_SCHEDULER_ROLE"
	runtimeRoleBot               = "bot"
	runtimeRoleWorker            = "worker"
	schedulerRoleOff             = "off"
)

func runtimeAllowsAlarmScheduler(runtimeRole, configuredRole string) bool {
	role := strings.ToLower(strings.TrimSpace(configuredRole))
	if role == "" {
		role = strings.ToLower(strings.TrimSpace(runtimeRole))
	}

	switch role {
	case schedulerRoleOff:
		return false
	case runtimeRoleBot, runtimeRoleWorker:
		return strings.ToLower(strings.TrimSpace(runtimeRole)) == role
	default:
		return false
	}
}

func buildAlarmWorkerRuntimeScheduler(
	runtimeRole string,
	cfg *config.Config,
	infra *appbootstrap.AlarmWorkerInfrastructure,
	logger *slog.Logger,
	configuredRole string,
) (runtimeAlarmScheduler, error) {
	if !runtimeAllowsAlarmScheduler(runtimeRole, configuredRole) {
		if logger != nil {
			logger.Info(
				"Alarm runtime scheduler disabled for this runtime",
				slog.String("runtime_role", runtimeRole),
				slog.String("configured_role", strings.TrimSpace(configuredRole)),
			)
		}
		return nil, nil
	}

	scheduler, err := appbootstrap.NewAlarmWorkerRuntimeScheduler(
		cfg,
		infra.Cache,
		infra.HolodexService,
		infra.ChzzkClient,
		infra.TwitchClient,
		infra.AlarmCRUD,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}
