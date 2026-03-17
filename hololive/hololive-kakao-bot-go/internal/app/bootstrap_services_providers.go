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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// ProvideACLService - 접근 제어 서비스 생성 (PostgreSQL 영구화).
func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoACLMode acl.ACLMode,
	kakaoRooms []string,
	postgres database.Client,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*acl.Service, error) {
	svc, err := acl.NewACLService(
		ctx,
		postgres,
		cacheSvc,
		logger,
		kakaoACLEnabled,
		kakaoACLMode,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACL service: %w", err)
	}

	return svc, nil
}

// ProvideActivityLogger - 활동 로거 생성 (stdout-only, 파일 로깅 제거됨).
func ProvideActivityLogger(logger *slog.Logger) *activity.Logger {
	return activity.NewActivityLogger("", logger)
}

// ProvideBotDependencies - 모든 의존성을 bot.Dependencies로 조립.
func ProvideBotDependencies(modules botDependencyModules) *bot.Dependencies {
	var youTubeStatsRepo stats.StatsCommandRepository

	if statsRepo := modules.stream.ytStack.GetStatsRepo(); statsRepo != nil {
		youTubeStatsRepo = statsRepo
	}

	var (
		youTubeService   = modules.stream.ytStack.GetService()
		youTubeScheduler = modules.stream.ytStack.GetScheduler()
	)

	return &bot.Dependencies{
		BotSelfUser:      modules.core.botSelfUser,
		IrisBaseURL:      modules.core.irisBaseURL,
		Notification:     modules.core.notification,
		Logger:           modules.core.logger,
		Client:           modules.messaging.client,
		MessageAdapter:   modules.messaging.messageAdapter,
		Formatter:        modules.messaging.formatter,
		Cache:            modules.data.cacheSvc,
		Postgres:         modules.data.postgres,
		MemberRepo:       modules.data.memberRepo,
		MemberCache:      modules.data.memberCache,
		Holodex:          modules.stream.holodexSvc,
		Chzzk:            modules.stream.chzzkClient,
		Twitch:           modules.stream.twitchClient,
		Profiles:         modules.data.profiles,
		Alarm:            modules.stream.alarmSvc,
		Matcher:          modules.stream.memberMatch,
		MembersData:      modules.data.membersData,
		Service:          youTubeService,
		Scheduler:        youTubeScheduler,
		YouTubeStatsRepo: youTubeStatsRepo,
			Activity:         modules.support.activityLogger,
			Settings:         modules.support.settingsSvc,
			ACL:              modules.support.aclSvc,
			MajorEventRepo:   modules.feature.majorEventRepo,
			MemberNews:       modules.feature.memberNewsSvc,
			CommandFactories: modules.feature.commandFactories,
			WorkerPool:       modules.support.workerPool,
		}
}
