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

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

func BuildAlarmWorkerConfigSubscriber(
	ctx context.Context,
	infra *appbootstrap.CoreInfrastructure,
	logger *slog.Logger,
) *configsub.Subscriber {
	if infra == nil || infra.Deps == nil || infra.Deps.Cache == nil || infra.AlarmCRUD == nil {
		return nil
	}

	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			targets := infra.AlarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
			if logger != nil {
				logger.Info(
					"Alarm worker applied alarm advance minutes via pub/sub",
					slog.Int("minutes", payload.Minutes),
					slog.Any("targets", targets),
				)
			}
		},
	})

	return configsub.New(infra.Deps.Cache.GetClient(), applyFn, logger)
}
