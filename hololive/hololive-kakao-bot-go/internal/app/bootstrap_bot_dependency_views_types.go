package app

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// botIngestionRuntimeDependencies: ingestion 런타임 조립에 필요한 최소 의존성 뷰.
type botIngestionRuntimeDependencies struct {
	cache      cache.Client
	postgres   database.Client
	irisClient iris.Client
	members    member.DataProvider
	scheduler  youtube.Scheduler
	settings   settings.ReadWriter
}

// botWebhookRuntimeDependencies: webhook 핸들러 조립에 필요한 최소 의존성 뷰.
type botWebhookRuntimeDependencies struct {
	cache cache.Client
}

// botConfigSubscriberDependencies: 설정 구독 적용에 필요한 최소 의존성 뷰.
type botConfigSubscriberDependencies struct {
	cache    cache.Client
	settings settings.ReadWriter
}

// botConfigSubscriberRuntimeDependencies: 설정 적용 핸들러가 참조하는 런타임 의존성 뷰.
type botConfigSubscriberRuntimeDependencies struct {
	youtubeService youtube.Service
	holodexService *holodex.Service
	alarmCRUD      domain.AlarmCRUD
}

// botYouTubeRuntimeDependencies: YouTube 수집 컴포넌트 조립에서 참조하는 런타임 의존성 뷰.
type botYouTubeRuntimeDependencies struct {
	sharedRateLimiter *scraper.RateLimiter
	templateRenderer  *template.Renderer
	youtubeService    youtube.Service
	holodexService    *holodex.Service
	photoSyncService  *holodex.PhotoSyncService
}

// botAdminRuntimeDependencies: admin API 조립에 필요한 최소 의존성 뷰.
type botAdminRuntimeDependencies struct {
	cache            cache.Client
	postgres         database.Client
	memberRepo       *member.Repository
	memberCache      *member.Cache
	profiles         *member.ProfileService
	alarmCRUD        domain.AlarmCRUD
	holodexService   *holodex.Service
	youtubeService   youtube.Service
	statsRepo        youtube.StatsDashboardRepository
	activityLogger   *activity.Logger
	settings         settings.ReadWriter
	acl              *acl.Service
	templateAdminSvc *template.AdminService
}

// botServerRuntimeDependencies: HTTP 서버 조립에서 필요한 런타임 의존성 뷰.
type botServerRuntimeDependencies struct {
	alarmCRUD domain.AlarmCRUD
}

// botRuntimeDependencyViews: buildBotRuntime에서 소비하는 의존성 뷰 집합.
type botRuntimeDependencyViews struct {
	botDeps                 *bot.Dependencies
	ingestion               botIngestionRuntimeDependencies
	webhook                 botWebhookRuntimeDependencies
	configSubscriber        botConfigSubscriberDependencies
	configSubscriberRuntime botConfigSubscriberRuntimeDependencies
	youtubeRuntime          botYouTubeRuntimeDependencies
	adminRuntime            botAdminRuntimeDependencies
	serverRuntime           botServerRuntimeDependencies
}
