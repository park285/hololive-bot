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
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	messagingadapter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/ingress"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/lifecycle"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type Bot struct {
	botSelfUser           string
	irisBaseURL           string
	notification          settings.NotificationConfig
	logger                *slog.Logger
	irisClient            BotIrisClient
	messageAdapter        *messagingadapter.MessageAdapter
	formatter             *formatter.ResponseFormatter
	messageStrings        *messagestrings.Store
	markdownReplies       bool
	cache                 cache.Client
	postgres              database.Client
	holodex               domain.StreamProvider
	alarm                 handlercore.AlarmService
	matcher               *matcher.Matcher
	commandRegistry       *command.Registry
	acl                   *acl.Service
	majorEventRepository  handlercore.MajorEventRepository
	memberNews            handlercore.MemberNewsService
	commandBuilders       []orchcmd.CommandBuilder
	membersData           domain.MemberDataProvider
	memberRepository      handlercore.CelebrationCalendarFinder
	calendarImageRenderer handlercore.CalendarImageRenderer
	stopCh                chan struct{}
	doneCh                chan struct{}
	selfSender            string
	ingress               *ingress.MessageIngress
	commandExecutor       *orchcmd.CommandRouter
	transport             *transport.CommandTransport
	lifecycle             *lifecycle.BotLifecycle
	rooms                 *kakaoroom.Catalog
}

func NewBot(deps *Dependencies) (*Bot, error) {
	if err := validateBotDependencies(deps); err != nil {
		return nil, err
	}

	var calendarFinder handlercore.CelebrationCalendarFinder

	if deps.MemberRepository != nil {
		calendarFinder = command.NewCachedCelebrationCalendarFinder(deps.MemberRepository, deps.CalendarImageCacheDir, deps.CalendarEntryCacheTTL)
	}

	bot := &Bot{
		botSelfUser:          deps.BotSelfUser,
		irisBaseURL:          deps.IrisBaseURL,
		notification:         deps.Notification,
		logger:               deps.Logger,
		irisClient:           deps.Client,
		messageAdapter:       deps.MessageAdapter,
		formatter:            deps.Formatter,
		messageStrings:       deps.MessageStrings,
		markdownReplies:      deps.MarkdownReplies,
		cache:                deps.Cache,
		postgres:             deps.Postgres,
		holodex:              deps.Holodex,
		alarm:                deps.Alarm,
		matcher:              deps.Matcher,
		acl:                  deps.ACL,
		majorEventRepository: deps.MajorEventRepository,
		memberNews:           deps.MemberNews,
		commandBuilders:      orchcmd.CloneCommandBuilders(deps.CommandBuilders),
		membersData:          deps.MembersData,
		memberRepository:     calendarFinder,
		stopCh:               make(chan struct{}),
		doneCh:               make(chan struct{}),
		selfSender:           stringutil.Normalize(deps.BotSelfUser),
	}
	bot.initImageRenderers(deps.CalendarImageCacheDir, deps.MessageStrings)

	bot.rooms = newRoomCatalog(bot.postgres, bot.irisClient, bot.logger)

	bot.transport = bot.newCommandTransport()
	bot.ingress = ingress.NewMessageIngress(bot.messageAdapter, bot.acl, bot.logger, bot.selfSender, ingress.WithRoomObserver(bot.rooms))
	bot.lifecycle = lifecycle.NewBotLifecycle(
		bot.logger,
		bot.cache,
		bot.irisClient,
		bot.irisBaseURL,
		bot.stopCh,
		bot.doneCh,
	)

	bot.initializeCommands()

	return bot, nil
}

func (b *Bot) initImageRenderers(calendarCacheDir string, strings *messagestrings.Store) {
	b.calendarImageRenderer = render.NewCalendarCardRenderer(render.WithCalendarDiskCacheDir(calendarCacheDir), render.WithCalendarStrings(strings))
}

func (b *Bot) initializeCommands() {
	registry := command.NewRegistry()

	b.commandRegistry = registry

	view := b.commandInitView()
	deps := view.toCommandDependencies(registry)

	commandsList := view.buildCommands(deps)
	for _, cmd := range commandsList {
		registry.Register(cmd)
	}

	b.commandExecutor = orchcmd.NewCommandRouter(registry, b.logger, b.sendMessage, b.messageStrings, b.cache)
	b.logger.Info("Commands initialized", slog.Int("count", registry.Count()))
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.ensureLifecycle().Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown(ctx context.Context) error {
	if err := b.ensureLifecycle().Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
