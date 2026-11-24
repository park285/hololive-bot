package handlercore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Handler[P botcommand.Params] interface {
	Name() string
	Description() string
	Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params P) error
}

type Command interface {
	Name() string
	Description() string
	Dispatch(ctx context.Context, cmdCtx *botcommand.CommandContext, params botcommand.Params) error
}

var ErrParamsMismatch = errors.New("command params type mismatch")

func Bind[P botcommand.Params](handler Handler[P]) Command {
	return boundHandler[P]{handler}
}

type boundHandler[P botcommand.Params] struct {
	Handler[P]
}

func (b boundHandler[P]) Dispatch(ctx context.Context, cmdCtx *botcommand.CommandContext, params botcommand.Params) error {
	var typed P

	if params != nil {
		var ok bool

		if typed, ok = params.(P); !ok {
			return fmt.Errorf("%w: %s received %T, want %T", ErrParamsMismatch, b.Name(), params, typed)
		}
	}

	return b.Execute(ctx, cmdCtx, typed)
}

type CelebrationCalendarFinder interface {
	FindMembersWithCelebrationsInMonth(ctx context.Context, month, referenceYear int) ([]domain.CalendarEntry, error)
}

type CalendarImageRenderer interface {
	RenderCalendarImagesContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([][]byte, error)
}

type HelpImageProvider interface {
	HelpImages(ctx context.Context) ([][]byte, error)
}

type SchedulePosterReader interface {
	PublishedMemberRows(ctx context.Context, memberID int, channelID string, day time.Time) (scheduleposter.PublishedSchedule, cardtemplate.ID, bool, error)
}

type ScheduleCardRenderer interface {
	RenderScheduleCards(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID) ([][]byte, error)
}

type BroadcastHistoryQuery struct {
	ChannelID string
	Type      string
	TopicID   string
	Limit     int
	Since     time.Time
}

type BroadcastThumbnailQuery struct {
	VideoID string
}

type BroadcastHistoryEntry struct {
	VideoID             string
	ChannelID           string
	MemberName          string
	Title               string
	TopicID             string
	ThumbnailURL        string
	BroadcastType       string
	ScheduledStartTime  *time.Time
	StartedAt           *time.Time
	EndedAt             *time.Time
	LastSeenAt          time.Time
	LifecycleOrigin     string
	LastLivePositiveAt  *time.Time
	TimesSource         string
	FinalizationOutcome *string
	FinalizationReason  *string
}

type BroadcastHistoryResult struct {
	Entries   []BroadcastHistoryEntry
	Truncated bool
}

var ErrBroadcastNotFound = errors.New("broadcast not found")

type BroadcastHistoryRepository interface {
	ListEndedBroadcasts(ctx context.Context, query *BroadcastHistoryQuery) (BroadcastHistoryResult, error)
	GetEndedBroadcast(ctx context.Context, query BroadcastThumbnailQuery) (*BroadcastHistoryEntry, error)
}

var ErrBroadcastThumbnailNotFound = errors.New("broadcast thumbnail not found")

type BroadcastThumbnailDownloader interface {
	Download(ctx context.Context, entry *BroadcastHistoryEntry) ([]byte, string, error)
}

type AlarmService interface {
	AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error)
	RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error)
	RemoveHostAlarm(context.Context, string, string, string, domain.AlarmTypes) (bool, error)
	ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error)
	ClearRoomAlarms(context.Context, string) (int, error)
}

type CollabMemberReader interface {
	DisplayNames(ctx context.Context, videos []collabmembers.Video) (map[string][]string, error)
}

type Dependencies struct {
	Holodex             domain.StreamProvider
	LiveQuery           livequery.Reader
	CollabMembers       CollabMemberReader
	Alarm               AlarmService
	Matcher             *matcher.Matcher
	BroadcastHistory    BroadcastHistoryRepository
	ThumbnailDownloader BroadcastThumbnailDownloader
	MembersData         domain.MemberDataProvider
	Formatter           *formatter.ResponseFormatter
	HelpImageProvider   HelpImageProvider
	SchedulePosters     SchedulePosterReader
	ScheduleCards       ScheduleCardRenderer
	SendMessage         func(ctx context.Context, room, message string) error
	SendImage           func(ctx context.Context, room string, imageData []byte) error
	SendTextFile        func(ctx context.Context, room, fileName, text string) error
	SendImages          func(ctx context.Context, room string, images [][]byte) error
	SendError           func(ctx context.Context, room, message string) error
	Logger              *slog.Logger
}
