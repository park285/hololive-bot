package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

type BotInfrastructure struct {
	Deps           *bot.Dependencies
	AlarmCRUD      domain.AlarmCRUD
	HolodexService *holodex.Service
	IrisRoomLister IrisRoomLister
	Postgres       database.Client
	Cache          cache.Client
	Cleanup        func()
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

type AlarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	AlarmService     *notification.AlarmService
	ChzzkClient      *chzzk.Client
	TwitchClient     *twitch.Client
	MemberDataSource member.DataProvider
}

type AlarmDependencies struct {
	AlarmService       *notification.AlarmService
	MemberDataProvider member.DataProvider
	ChzzkClient        *chzzk.Client
	TwitchClient       *twitch.Client
}

type ScraperHolodexFoundation struct {
	HolodexService       *holodex.Service
	MemberServiceAdapter member.DataProvider
	SharedRL             *scraper.RateLimiter
}

type ScraperHolodexProfileFoundation struct {
	HolodexService       *holodex.Service
	MemberServiceAdapter member.DataProvider
	ProfileService       *member.ProfileService
	SharedRL             *scraper.RateLimiter
}

type CoreIntegrationServices struct {
	ACLService           *acl.Service
	MajorEventRepository command.MajorEventRepository
	MemberNewsService    command.MemberNewsService
	CommandBuilders      []bot.CommandBuilder
	WorkerPool           *workerpool.QueuedPool
}

type BotCoreModule struct {
	BotSelfUser           string
	IrisBaseURL           string
	Notification          config.NotificationConfig
	CalendarImageCacheDir string
	CalendarEntryCacheTTL time.Duration
	Logger                *slog.Logger
}

type BotMessagingModule struct {
	Client         iris.BotClient
	MessageAdapter *adapter.MessageAdapter
	Formatter      *adapter.ResponseFormatter
	MessageStrings *messagestrings.Store
}

type BotDataModule struct {
	Cache            cache.Client
	Postgres         database.Client
	MemberRepository *member.Repository
	MemberCache      *member.Cache
	Profiles         *member.ProfileService
	MembersData      member.DataProvider
}

type BotStreamModule struct {
	Holodex      *holodex.Service
	ChzzkClient  *chzzk.Client
	TwitchClient *twitch.Client
	Alarm        domain.AlarmCRUD
	MemberMatch  *matcher.Matcher
	YTStack      *providers.YouTubeStack
}

type BotSupportModule struct {
	ActivityLogger *activity.Logger
	Settings       settings.ReadWriter
	ACL            *acl.Service
	WorkerPool     *workerpool.QueuedPool
}

type BotFeatureModule struct {
	MajorEventRepository command.MajorEventRepository
	MemberNews           command.MemberNewsService
	CommandBuilders      []bot.CommandBuilder
}

type BotDependencyModules struct {
	Core      BotCoreModule
	Messaging BotMessagingModule
	Data      BotDataModule
	Stream    BotStreamModule
	Support   BotSupportModule
	Feature   BotFeatureModule
}

type BotWebhookRuntimeDependencies struct {
	Cache cache.Client
}

type BotConfigSubscriberDependencies struct {
	Cache    cache.Client
	Settings settings.ReadWriter
}

type BotConfigSubscriberRuntimeDependencies struct {
	YouTubeService youtube.Service
	HolodexService *holodex.Service
	AlarmCRUD      domain.AlarmCRUD
}
