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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

func ProvideAlarmService(
	advanceMinutes []int,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepo *alarm.Repository,
	logger *slog.Logger,
) (*notification.AlarmService, error) {
	return appbootstrap.ProvideAlarmService(advanceMinutes, cacheSvc, holodexSvc, chzzkClient, twitchClient, memberData, alarmRepo, logger)
}

func ProvideAlarmRepository(postgres database.Client, logger *slog.Logger) *alarm.Repository {
	return appbootstrap.ProvideAlarmRepository(postgres, logger)
}

func ProvideAlarmWorkerPool() (*workerpool.Pool, error) {
	return appbootstrap.ProvideAlarmWorkerPool()
}

func ProvideMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	logger *slog.Logger,
) *matcher.MemberMatcher {
	return appbootstrap.ProvideMemberMatcher(ctx, membersData, cacheSvc, holodexSvc, logger)
}
