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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/info"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

type commandInitView struct {
	holodex               domain.StreamProvider
	postgres              database.Client
	alarm                 handlercore.AlarmService
	matcher               *matcher.Matcher
	membersData           domain.MemberDataProvider
	formatter             *formatter.ResponseFormatter
	sendMessage           func(ctx context.Context, room, message string) error
	sendImage             func(ctx context.Context, room string, imageData []byte) error
	sendImages            func(ctx context.Context, room string, images [][]byte) error
	sendError             func(ctx context.Context, room, message string) error
	logger                *slog.Logger
	memberRepository      handlercore.CelebrationCalendarFinder
	calendarImageRenderer handlercore.CalendarImageRenderer
}

func (b *Bot) commandInitView(deps *Dependencies, calendarFinder handlercore.CelebrationCalendarFinder) commandInitView {
	return commandInitView{
		holodex:               deps.Holodex,
		postgres:              b.postgres,
		alarm:                 deps.Alarm,
		matcher:               deps.Matcher,
		membersData:           deps.MembersData,
		formatter:             b.formatter,
		sendMessage:           b.sendMessage,
		sendImage:             b.sendImage,
		sendImages:            b.sendImages,
		sendError:             b.sendError,
		logger:                b.logger,
		memberRepository:      calendarFinder,
		calendarImageRenderer: b.calendarImageRenderer,
	}
}

func (v *commandInitView) toCommandDependencies() *handlercore.Dependencies {
	return &handlercore.Dependencies{
		Holodex:             v.holodex,
		LiveQuery:           livequery.New(v.postgres),
		CollabMembers:       collabmembers.New(v.postgres, v.membersData),
		Alarm:               v.alarm,
		Matcher:             v.matcher,
		BroadcastHistory:    handlers.NewPgBroadcastHistoryRepository(v.postgres),
		ThumbnailDownloader: handlers.NewYouTubeThumbnailDownloader(nil),
		MembersData:         v.membersData,
		Formatter:           v.formatter,
		HelpImageProvider:   render.NewHelpCardProvider(),
		SchedulePosters:     schedulePosterReader{postgres: v.postgres},
		SendMessage:         v.sendMessage,
		SendImage:           v.sendImage,
		SendImages:          v.sendImages,
		SendError:           v.sendError,
		Logger:              v.logger,
	}
}

func (v *commandInitView) buildCommands(deps *handlercore.Dependencies) []handlercore.Command {
	commands := []handlercore.Command{
		handlers.NewHelpCommand(deps),
		handlers.NewLiveCommand(deps),
		handlers.NewUpcomingCommand(deps),
		handlers.NewScheduleCommand(deps),
		handlers.NewSchedulePosterCommand(deps),
		alarmcmd.NewAlarmCommand(deps),
		info.NewMemberInfoCommand(deps),
		handlers.NewBroadcastHistoryCommand(deps),
		handlers.NewBroadcastThumbnailCommand(deps),
		handlers.NewBroadcastTimeCommand(deps),
	}

	if v.memberRepository != nil {
		commands = append(commands, handlers.NewCalendarCommand(deps, v.memberRepository, v.calendarImageRenderer))
	}

	return commands
}

// schedulePosterReader는 호출 시점의 pool로 읽어, pool이 없으면 조용히 끄지 않고 조회 오류로 드러낸다.
type schedulePosterReader struct {
	postgres database.Client
}

func (r schedulePosterReader) PublishedImage(ctx context.Context, channelID string, day time.Time) (scheduleposter.Image, bool, error) {
	if r.postgres == nil {
		return scheduleposter.Image{}, false, errors.New("schedule poster reader requires database client")
	}

	store, err := scheduleposter.NewStore(r.postgres.GetPool())
	if err != nil {
		return scheduleposter.Image{}, false, fmt.Errorf("open schedule poster store: %w", err)
	}

	image, found, err := store.PublishedImage(ctx, channelID, day)
	if err != nil {
		return scheduleposter.Image{}, false, fmt.Errorf("read schedule poster: %w", err)
	}

	return image, found, nil
}
