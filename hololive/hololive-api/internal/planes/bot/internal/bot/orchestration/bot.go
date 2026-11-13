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
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

type Bot struct {
	irisBaseURL           string
	logger                *slog.Logger
	irisClient            BotIrisClient
	messageAdapter        *messagingadapter.MessageAdapter
	formatter             *formatter.ResponseFormatter
	messageStrings        *messagestrings.Store
	markdownReplies       bool
	cache                 BotCache
	postgres              database.Client
	acl                   *acl.Service
	calendarImageRenderer handlercore.CalendarImageRenderer
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

	calendarCache := sharedcache.NewValkeyStore(deps.Cache)
	calendarFinder := command.NewCachedCelebrationCalendarFinder(deps.MemberRepository, calendarCache, deps.CalendarEntryCacheTTL)

	bot := &Bot{
		irisBaseURL:     deps.IrisBaseURL,
		logger:          deps.Logger,
		irisClient:      deps.Client,
		messageAdapter:  deps.MessageAdapter,
		formatter:       deps.Formatter,
		messageStrings:  deps.MessageStrings,
		markdownReplies: deps.MarkdownReplies,
		cache:           deps.Cache,
		postgres:        deps.Postgres,
		acl:             deps.ACL,
		selfSender:      stringutil.Normalize(deps.BotSelfUser),
	}
	bot.initImageRenderers(calendarCache, deps.MessageStrings)

	bot.rooms = kakaoroom.New(bot.postgres.GetPool(), kakaoroom.NewIrisLister(bot.irisClient), bot.logger)

	bot.transport = transport.NewCommandTransport(
		deps.ReplyOutbox,
		bot.formatter,
		transport.WithMarkdownReplies(bot.markdownReplies),
		transport.WithRoomChatLookup(bot.rooms),
	)
	bot.ingress = ingress.NewMessageIngress(bot.messageAdapter, bot.acl, bot.logger, bot.selfSender, ingress.WithRoomObserver(bot.rooms))
	bot.lifecycle = lifecycle.NewBotLifecycle(bot.logger, bot.cache, bot.irisClient, bot.irisBaseURL)

	if err := bot.initializeCommands(deps, calendarFinder); err != nil {
		return nil, err
	}

	return bot, nil
}

func (b *Bot) initImageRenderers(calendarCache sharedcache.Store, strings *messagestrings.Store) {
	b.calendarImageRenderer = render.NewCalendarCardRenderer(render.WithCalendarImageCache(calendarCache), render.WithCalendarStrings(strings))
}

func (b *Bot) initializeCommands(deps *Dependencies, calendarFinder handlercore.CelebrationCalendarFinder) error {
	registry := command.NewRegistry()

	schedulePosters, err := scheduleposter.NewStore(b.postgres.GetPool())
	if err != nil {
		return fmt.Errorf("initialize schedule poster store: %w", err)
	}

	view := b.commandInitView(deps, calendarFinder, schedulePosters)
	commandDeps := view.toCommandDependencies()

	for _, cmd := range view.buildCommands(commandDeps) {
		if err := cmd.CheckDependencies(); err != nil {
			return fmt.Errorf("initialize command %s: %w", cmd.Name(), err)
		}

		registry.Register(cmd)
	}

	b.commandExecutor = orchcmd.NewCommandRouter(registry, b.logger, b.sendMessage, b.messageStrings, b.cache)
	b.logger.Info("Commands initialized", slog.Int("count", registry.Count()))

	return nil
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.lifecycle.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown(ctx context.Context) error {
	if err := b.lifecycle.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
