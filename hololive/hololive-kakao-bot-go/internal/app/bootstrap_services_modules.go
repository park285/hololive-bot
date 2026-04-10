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
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

func buildBotDependencyModules(
	cfg *config.Config,
	infra *infraResources,
	alarmMode *alarmModeComponents,
	holodexService *holodex.Service,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	irisClient botIrisClient,
	profileService *member.ProfileService,
	memberMatcher *matcher.MemberMatcher,
	youTubeStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	aclService *acl.Service,
	majorEventRepo command.MajorEventRepository,
	memberNewsService command.MemberNewsService,
	commandBuilders []bot.CommandBuilder,
	workerPool *workerpool.Pool,
	logger *slog.Logger,
) botDependencyModules {
	return botDependencyModules{
		core: botCoreModule{
			botSelfUser:  cfg.Bot.SelfUser,
			irisBaseURL:  cfg.Iris.BaseURL,
			notification: cfg.Notification,
			logger:       logger,
		},
		messaging: botMessagingModule{
			client:         irisClient,
			messageAdapter: messageAdapter,
			formatter:      formatter,
		},
		data: botDataModule{
			cacheSvc:    infra.Cache,
			postgres:    infra.Postgres,
			memberRepo:  infra.MemberRepo,
			memberCache: infra.MemberCache,
			profiles:    profileService,
			membersData: alarmMode.memberDataSource,
		},
		stream: botStreamModule{
			holodexSvc:   holodexService,
			chzzkClient:  alarmMode.chzzkClient,
			twitchClient: alarmMode.twitchClient,
			alarmSvc:     alarmMode.alarmCRUD,
			memberMatch:  memberMatcher,
			ytStack:      youTubeStack,
		},
		support: botSupportModule{
			activityLogger: activityLogger,
			settingsSvc:    settingsService,
			aclSvc:         aclService,
			workerPool:     workerPool,
		},
		feature: botFeatureModule{
			majorEventRepo:  majorEventRepo,
			memberNewsSvc:   memberNewsService,
			commandBuilders: cloneCommandBuilders(commandBuilders),
		},
	}
}
