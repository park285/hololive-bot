package orchestration

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

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
)

type commandInitView struct {
	holodex               domain.StreamProvider
	pool                  *pgxpool.Pool
	schedulePosters       handlercore.SchedulePosterReader
	alarm                 handlercore.AlarmService
	matcher               *matcher.Matcher
	membersData           domain.MemberDataProvider
	formatter             *formatter.ResponseFormatter
	sendMessage           func(ctx context.Context, room, message string) error
	sendImage             func(ctx context.Context, room string, imageData []byte) error
	sendTextFile          func(ctx context.Context, room, fileName, text string) error
	sendImages            func(ctx context.Context, room string, images [][]byte) error
	sendError             func(ctx context.Context, room, message string) error
	logger                *slog.Logger
	memberRepository      handlercore.CelebrationCalendarFinder
	calendarImageRenderer handlercore.CalendarImageRenderer
	scheduleCards         handlercore.ScheduleCardRenderer
}

func (b *Bot) commandInitView(deps *Dependencies, calendarFinder handlercore.CelebrationCalendarFinder, schedulePosters handlercore.SchedulePosterReader) commandInitView {
	return commandInitView{
		holodex:               deps.Holodex,
		pool:                  b.postgres.GetPool(),
		schedulePosters:       schedulePosters,
		alarm:                 deps.Alarm,
		matcher:               deps.Matcher,
		membersData:           deps.MembersData,
		formatter:             b.formatter,
		sendMessage:           b.sendMessage,
		sendImage:             b.sendImage,
		sendTextFile:          b.transport.SendTextFile,
		sendImages:            b.sendImages,
		sendError:             b.sendError,
		logger:                b.logger,
		memberRepository:      calendarFinder,
		calendarImageRenderer: b.calendarImageRenderer,
		scheduleCards:         b.scheduleCardRenderer,
	}
}

func (v *commandInitView) toCommandDependencies() *handlercore.Dependencies {
	return &handlercore.Dependencies{
		Holodex:             v.holodex,
		LiveQuery:           livequery.New(v.pool),
		CollabMembers:       collabmembers.New(v.pool, v.membersData),
		Alarm:               v.alarm,
		Matcher:             v.matcher,
		BroadcastHistory:    handlers.NewPgBroadcastHistoryRepository(v.pool),
		ThumbnailDownloader: handlers.NewYouTubeThumbnailDownloader(nil),
		MembersData:         v.membersData,
		Formatter:           v.formatter,
		HelpImageProvider:   render.NewHelpCardProvider(),
		SchedulePosters:     v.schedulePosters,
		ScheduleCards:       v.scheduleCards,
		SendMessage:         v.sendMessage,
		SendImage:           v.sendImage,
		SendTextFile:        v.sendTextFile,
		SendImages:          v.sendImages,
		SendError:           v.sendError,
		Logger:              v.logger,
	}
}

type checkedCommand interface {
	handlercore.Command
	CheckDependencies() error
}

func (v *commandInitView) buildCommands(deps *handlercore.Dependencies) []checkedCommand {
	return []checkedCommand{
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
		handlers.NewCalendarCommand(deps, v.memberRepository, v.calendarImageRenderer),
	}
}
