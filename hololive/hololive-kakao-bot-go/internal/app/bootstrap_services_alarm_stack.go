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

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
)

type alarmYouTubeStackComponents struct {
	alarmMode       *alarmModeComponents
	memberMatcher   *matcher.MemberMatcher
	youTubeStack    *sharedproviders.YouTubeStack
	activityLogger  *activity.Logger
	settingsService settings.ReadWriter
}

func initAlarmYouTubeStack(
	ctx context.Context,
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	irisClient iris.Sender,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*alarmYouTubeStackComponents, error) {
	components, err := appbootstrap.InitAlarmYouTubeStack(ctx, cfg, infra, foundation, irisClient, formatter, logger)
	if err != nil {
		return nil, err
	}
	return &alarmYouTubeStackComponents{
		alarmMode:       components.AlarmMode,
		memberMatcher:   components.MemberMatcher,
		youTubeStack:    components.YouTubeStack,
		activityLogger:  components.ActivityLogger,
		settingsService: components.SettingsService,
	}, nil
}
