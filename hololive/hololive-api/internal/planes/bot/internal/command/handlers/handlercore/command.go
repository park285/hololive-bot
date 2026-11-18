package handlercore

import (
	"context"
	"errors"
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

type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error
}

type CelebrationCalendarFinder interface {
	FindMembersWithCelebrationsInMonth(ctx context.Context, month, referenceYear int) ([]domain.CalendarEntry, error)
}

type CalendarImageRenderer interface {
	RenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error)
}

type HelpImageProvider interface {
	HelpImages(ctx context.Context) ([][]byte, error)
}

type SchedulePosterReader interface {
	MemberCardTemplate(ctx context.Context, memberID int) (cardtemplate.ID, error)
	PublishedRows(ctx context.Context, channelID string, day time.Time) (scheduleposter.PublishedSchedule, bool, error)
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
