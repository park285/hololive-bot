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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type commandInitView struct {
	holodex               domain.StreamProvider
	chzzk                 *chzzk.Client
	cache                 cache.Client
	alarm                 domain.AlarmCRUD
	matcher               *matcher.Matcher
	officialProfiles      *member.ProfileService
	statsRepository       stats.StatsCommandRepository
	memberNews            command.MemberNewsService
	membersData           member.DataProvider
	formatter             *adapter.ResponseFormatter
	sendMessage           func(ctx context.Context, room, message string) error
	sendImage             func(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error
	sendError             func(ctx context.Context, room, message string) error
	logger                *slog.Logger
	majorEventRepository  command.MajorEventRepository
	memberRepository      command.CelebrationCalendarFinder
	calendarImageRenderer command.CalendarImageRenderer
	commandBuilders       []orchcmd.CommandBuilder
}

func (b *Bot) commandInitView() commandInitView {
	if b == nil {
		return commandInitView{}
	}

	return commandInitView{
		holodex:               b.holodex,
		chzzk:                 b.chzzk,
		cache:                 b.cache,
		alarm:                 b.alarm,
		matcher:               b.matcher,
		officialProfiles:      b.officialProfiles,
		statsRepository:       b.statsRepository,
		memberNews:            b.memberNews,
		membersData:           b.membersData,
		formatter:             b.formatter,
		sendMessage:           b.sendMessage,
		sendImage:             b.sendImage,
		sendError:             b.sendError,
		logger:                b.logger,
		majorEventRepository:  b.majorEventRepository,
		memberRepository:      b.memberRepository,
		calendarImageRenderer: b.calendarImageRenderer,
		commandBuilders:       orchcmd.CloneCommandBuilders(b.commandBuilders),
	}
}

func (v *commandInitView) toCommandDependencies(registry *command.Registry) *command.Dependencies {
	deps := &command.Dependencies{
		Holodex:          v.holodex,
		Chzzk:            v.chzzk,
		Cache:            v.cache,
		Alarm:            v.alarm,
		Matcher:          v.matcher,
		OfficialProfiles: v.officialProfiles,
		StatsRepository:  v.statsRepository,
		MemberNews:       v.memberNews,
		MembersData:      v.membersData,
		Formatter:        v.formatter,
		SendMessage:      v.sendMessage,
		SendImage:        v.sendImage,
		SendError:        v.sendError,
		Logger:           v.logger,
	}

	deps.Dispatcher = command.NewSequentialDispatcher(registry, orchcmd.NormalizeCommandKey)

	return deps
}

func (v *commandInitView) buildCommands(deps *command.Dependencies) []command.Command {
	commands := []command.Command{
		command.NewHelpCommand(deps),
		command.NewLiveCommand(deps),
		command.NewUpcomingCommand(deps),
		command.NewScheduleCommand(deps),
		command.NewAlarmCommand(deps),
		command.NewMemberInfoCommand(deps),
		command.NewSubscriberCommand(deps),
		command.NewStatsCommand(deps),
	}

	if v.memberRepository != nil {
		commands = append(commands, command.NewCalendarCommand(deps, v.memberRepository, v.calendarImageRenderer))
	}
	if v.majorEventRepository != nil {
		v.logInfo("MajorEvent command enabled")

		commands = append(commands, command.NewMajorEventCommand(deps, v.majorEventRepository))
	}

	if v.memberNews != nil {
		v.logInfo("MemberNews commands enabled")

		commands = append(commands,
			command.NewMemberNewsCommand(deps),
			command.NewMemberNewsSubscriptionCommand(deps),
		)
	}

	commands = v.appendExternalCommands(commands, deps)
	return compactCommands(commands)
}

func (v *commandInitView) appendExternalCommands(commands []command.Command, deps *command.Dependencies) []command.Command {
	if len(v.commandBuilders) == 0 {
		return commands
	}
	v.logInfo("External command builders enabled", slog.Int("count", len(v.commandBuilders)))
	for _, builder := range v.commandBuilders {
		if builder != nil {
			commands = append(commands, builder(deps))
		}
	}
	return commands
}

func compactCommands(commands []command.Command) []command.Command {
	compacted := make([]command.Command, 0, len(commands))
	for _, cmd := range commands {
		if cmd == nil {
			continue
		}

		compacted = append(compacted, cmd)
	}

	return compacted
}

func (v *commandInitView) logInfo(msg string, args ...any) {
	if v.logger == nil {
		return
	}

	v.logger.Info(msg, args...)
}
