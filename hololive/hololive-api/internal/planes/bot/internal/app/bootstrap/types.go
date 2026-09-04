package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type BotInfrastructure struct {
	Deps           *orchestration.Dependencies
	AlarmCRUD      domain.AlarmCRUD
	AlarmService   *alarmservice.AlarmService
	HolodexService *holodexprovider.Service
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
	AlarmService     *alarmservice.AlarmService
	ChzzkClient      *chzzk.Client
	TwitchClient     *twitch.Client
	MemberDataSource domain.MemberDataProvider
}

type AlarmDependencies struct {
	AlarmService       *alarmservice.AlarmService
	MemberDataProvider domain.MemberDataProvider
	ChzzkClient        *chzzk.Client
	TwitchClient       *twitch.Client
}

type ScraperHolodexFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
	SharedRL             *ratelimiter.RateLimiter
}

type ScraperHolodexProfileFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
	ProfileService       *member.ProfileService
	SharedRL             *ratelimiter.RateLimiter
}

type CoreIntegrationServices struct {
	ACLService           *acl.Service
	MajorEventRepository handlercore.MajorEventRepository
	MemberNewsService    handlercore.MemberNewsService
	CommandBuilders      []orchcmd.CommandBuilder
}

type BotCoreModule struct {
	BotSelfUser           string
	IrisBaseURL           string
	Notification          configsettings.NotificationConfig
	CalendarImageCacheDir string
	CalendarEntryCacheTTL time.Duration
	Logger                *slog.Logger
}

type BotMessagingModule struct {
	Client          orchestration.BotIrisClient
	MessageAdapter  *messaging.MessageAdapter
	Formatter       *formatter.ResponseFormatter
	MessageStrings  *messagestrings.Store
	MarkdownReplies bool
}

type BotDataModule struct {
	Cache            cache.Client
	Postgres         database.Client
	MemberRepository *member.Repository
	MemberCache      *member.Cache
	Profiles         *member.ProfileService
	MembersData      domain.MemberDataProvider
}

type BotStreamModule struct {
	Holodex      *holodexprovider.Service
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
}

type BotFeatureModule struct {
	MajorEventRepository handlercore.MajorEventRepository
	MemberNews           handlercore.MemberNewsService
	CommandBuilders      []orchcmd.CommandBuilder
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
	HolodexService *holodexprovider.Service
	AlarmCRUD      domain.AlarmCRUD
	ACL            *acl.Service
}
