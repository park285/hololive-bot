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

package bot

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

type commandInitView struct {
	holodex          domain.StreamProvider
	chzzk            *chzzk.Client
	cache            cache.Client
	alarm            domain.AlarmCRUD
	matcher          *matcher.MemberMatcher
	officialProfiles *member.ProfileService
	statsRepo        stats.StatsCommandRepository
	memberNews       command.MemberNewsService
	membersData      member.DataProvider
	formatter        *adapter.ResponseFormatter
	sendMessage      func(ctx context.Context, room, message string) error
	sendImage        func(ctx context.Context, room, imageBase64 string) error
	sendError        func(ctx context.Context, room, message string) error
	logger           *slog.Logger
	majorEventRepo   command.MajorEventRepository
	commandFactories []command.Factory
}

func (b *Bot) commandInitView() commandInitView {
	if b == nil {
		return commandInitView{}
	}

	return commandInitView{
		holodex:          b.holodex,
		chzzk:            b.chzzk,
		cache:            b.cache,
		alarm:            b.alarm,
		matcher:          b.matcher,
		officialProfiles: b.officialProfiles,
		statsRepo:        b.statsRepo,
		memberNews:       b.memberNews,
		membersData:      b.membersData,
		formatter:        b.formatter,
		sendMessage:      b.sendMessage,
		sendImage:        b.sendImage,
		sendError:        b.sendError,
		logger:           b.logger,
		majorEventRepo:   b.majorEventRepo,
		commandFactories: append([]command.Factory(nil), b.commandFactories...),
	}
}

func (v commandInitView) toCommandDependencies(registry *command.Registry) *command.Dependencies {
	deps := &command.Dependencies{
		Holodex:          v.holodex,
		Chzzk:            v.chzzk,
		Cache:            v.cache,
		Alarm:            v.alarm,
		Matcher:          v.matcher,
		OfficialProfiles: v.officialProfiles,
		StatsRepo:        v.statsRepo,
		MemberNews:       v.memberNews,
		MembersData:      v.membersData,
		Formatter:        v.formatter,
		SendMessage:      v.sendMessage,
		SendImage:        v.sendImage,
		SendError:        v.sendError,
		Logger:           v.logger,
	}

	deps.Dispatcher = command.NewSequentialDispatcher(registry, normalizeCommandKey)

	return deps
}

func (v commandInitView) buildFactories() []command.Factory {
	factories := append([]command.Factory{}, command.DefaultFactories()...)

	if v.majorEventRepo != nil {
		v.logInfo("MajorEvent command enabled")

		factories = append(factories, command.NewMajorEventFactory(v.majorEventRepo))
	}

	if v.memberNews != nil {
		v.logInfo("MemberNews commands enabled")

		factories = append(factories, command.MemberNewsFactories()...)
	}

	if len(v.commandFactories) > 0 {
		v.logInfo("External command factories enabled", slog.Int("count", len(v.commandFactories)))

		factories = append(factories, v.commandFactories...)
	}

	return factories
}

func (v commandInitView) logInfo(msg string, args ...any) {
	if v.logger == nil {
		return
	}

	v.logger.Info(msg, args...)
}
