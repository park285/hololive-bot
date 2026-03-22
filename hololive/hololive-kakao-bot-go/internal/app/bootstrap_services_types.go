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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// coreInfrastructure 는 Bot 런타임 구성에 필요한 의존성/서비스 묶음을 담는다.
type coreInfrastructure struct {
	deps             *bot.Dependencies
	alarmService     *notification.AlarmService
	alarmCRUD        domain.AlarmCRUD
	holodexService   *holodex.Service // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack          *providers.YouTubeStack
	photoSync        *holodex.PhotoSyncService
	templateRenderer *template.Renderer
	templateAdminSvc *template.AdminService
	sharedRL         *scraper.RateLimiter // YouTube 전역 RateLimiter
	cleanupCache     func()
	cleanupDB        func()
}

type alarmModeComponents struct {
	alarmCRUD        domain.AlarmCRUD
	alarmService     *notification.AlarmService
	chzzkClient      *chzzk.Client
	twitchClient     *twitch.Client
	memberDataSource member.DataProvider
}

type alarmDependencies struct {
	alarmService       *notification.AlarmService
	memberDataProvider member.DataProvider
	chzzkClient        *chzzk.Client
	twitchClient       *twitch.Client
}

type botCoreModule struct {
	botSelfUser  string
	irisBaseURL  string
	notification config.NotificationConfig
	logger       *slog.Logger
}

type botMessagingModule struct {
	client         iris.Client
	messageAdapter *adapter.MessageAdapter
	formatter      *adapter.ResponseFormatter
}

type botDataModule struct {
	cacheSvc    cache.Client
	postgres    database.Client
	memberRepo  *member.Repository
	memberCache *member.Cache
	profiles    *member.ProfileService
	membersData member.DataProvider
}

type botStreamModule struct {
	holodexSvc   *holodex.Service
	chzzkClient  *chzzk.Client
	twitchClient *twitch.Client
	alarmSvc     domain.AlarmCRUD
	memberMatch  *matcher.MemberMatcher
	ytStack      *providers.YouTubeStack
}

type botSupportModule struct {
	activityLogger *activity.Logger
	settingsSvc    settings.ReadWriter
	aclSvc         *acl.Service
	workerPool     *workerpool.Pool
}

type botFeatureModule struct {
	majorEventRepo   command.MajorEventRepository
	memberNewsSvc    command.MemberNewsService
	commandFactories []command.Factory
}

type botDependencyModules struct {
	core      botCoreModule
	messaging botMessagingModule
	data      botDataModule
	stream    botStreamModule
	support   botSupportModule
	feature   botFeatureModule
}
