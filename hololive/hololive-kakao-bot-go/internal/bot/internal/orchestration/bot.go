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

package orchestration

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const (
	commandKeyAlarm            = "alarm"
	commandKeyNewsSubscription = "news_subscription"
)

type streamRuntime interface {
	domain.StreamProvider
	Stop()
}

type Bot struct {
	botSelfUser      string
	irisBaseURL      string
	notification     config.NotificationConfig
	logger           *slog.Logger
	irisClient       irisClient
	messageAdapter   *adapter.MessageAdapter
	formatter        *adapter.ResponseFormatter
	cache            cache.Client
	postgres         database.Client
	holodex          streamRuntime
	chzzk            *chzzk.Client
	twitch           *twitch.Client
	officialProfiles *member.ProfileService
	alarm            domain.AlarmCRUD
	matcher          *matcher.Matcher
	commandRegistry  *command.Registry
	statsRepository        stats.StatsCommandRepository
	acl              *acl.Service
	majorEventRepository   command.MajorEventRepository
	memberNews       command.MemberNewsService
	commandBuilders  []CommandBuilder
	membersData      member.DataProvider
	stopCh           chan struct{}
	doneCh           chan struct{}
	selfSender       string
	workerPool       *workerpool.Pool
	ingress          *MessageIngress
	commandExecutor  *CommandRouter
	transport        *CommandTransport
	lifecycle        *BotLifecycle
}

func NewBot(deps *Dependencies) (*Bot, error) {
	holodexRuntime, err := validateBotDependencies(deps)
	if err != nil {
		return nil, err
	}

	core := deps.coreDeps()
	messaging := deps.messagingDeps()
	data := deps.dataDeps()
	stream := deps.streamDeps()
	support := deps.supportDeps()
	feature := deps.featureDeps()

	bot := &Bot{
		botSelfUser:      core.botSelfUser,
		irisBaseURL:      core.irisBaseURL,
		notification:     core.notification,
		logger:           core.logger,
		irisClient:       messaging.client,
		messageAdapter:   messaging.messageAdapter,
		formatter:        messaging.formatter,
		cache:            data.cache,
		postgres:         data.postgres,
		holodex:          holodexRuntime,
		chzzk:            stream.chzzk,
		twitch:           stream.twitch,
		officialProfiles: stream.profiles,
		alarm:            stream.alarm,
		matcher:          stream.matcher,
		statsRepository:        stream.youTubeStatsRepository,
		acl:              support.acl,
		majorEventRepository:   feature.majorEventRepository,
		memberNews:       feature.memberNews,
		commandBuilders:  feature.commandBuilders,
		membersData:      stream.membersData,
		workerPool:       support.workerPool,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		selfSender:       stringutil.Normalize(core.botSelfUser),
	}

	bot.transport = NewCommandTransport(bot.irisClient, bot.formatter)
	bot.ingress = NewMessageIngress(bot.messageAdapter, bot.acl, bot.logger, bot.selfSender)
	bot.lifecycle = NewBotLifecycle(
		bot.logger,
		bot.cache,
		bot.irisClient,
		bot.irisBaseURL,
		bot.stopCh,
		bot.doneCh,
		bot.workerPool,
		bot.holodex,
		bot.postgres,
	)

	bot.initializeCommands()

	return bot, nil
}

func (b *Bot) initializeCommands() {
	registry := command.NewRegistry()

	b.commandRegistry = registry

	view := b.commandInitView()
	deps := view.toCommandDependencies(registry)

	b.logger.Info("Stats repository detected", slog.Bool("available", deps.StatsRepository != nil))

	commandsList := view.buildCommands(deps)
	for _, cmd := range commandsList {
		registry.Register(cmd)
	}

	b.commandExecutor = NewCommandRouter(registry, b.logger, b.sendMessage)
	b.logger.Info("Commands initialized", slog.Int("count", registry.Count()))
}

func (b *Bot) Start(ctx context.Context) error {
	return b.ensureLifecycle().Start(ctx)
}

func (b *Bot) waitUntilIrisReady(ctx context.Context, timeout, retryInterval, pingTimeout time.Duration) error {
	return b.ensureLifecycle().WaitUntilIrisReady(ctx, timeout, retryInterval, pingTimeout)
}

func (b *Bot) Shutdown(ctx context.Context) error {
	return b.ensureLifecycle().Shutdown(ctx)
}
