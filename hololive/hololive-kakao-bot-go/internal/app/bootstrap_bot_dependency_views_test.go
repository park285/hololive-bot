package app

import (
	"context"
	"testing"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type stubIrisClient struct{}

func (s *stubIrisClient) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	return nil
}
func (s *stubIrisClient) SendImage(ctx context.Context, room, imageBase64 string) error { return nil }
func (s *stubIrisClient) Ping(ctx context.Context) bool                                 { return true }
func (s *stubIrisClient) GetConfig(ctx context.Context) (*iris.Config, error)           { return nil, nil }
func (s *stubIrisClient) Decrypt(ctx context.Context, data string) (string, error)      { return data, nil }

type stubMemberDataProvider struct{}

func (s *stubMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member { return nil }
func (s *stubMemberDataProvider) FindMemberByName(name string) *domain.Member           { return nil }
func (s *stubMemberDataProvider) FindMemberByAlias(alias string) *domain.Member         { return nil }
func (s *stubMemberDataProvider) GetChannelIDs() []string                               { return nil }
func (s *stubMemberDataProvider) GetAllMembers() []*domain.Member                       { return nil }
func (s *stubMemberDataProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return s
}
func (s *stubMemberDataProvider) FindMembersByName(name string) []*domain.Member   { return nil }
func (s *stubMemberDataProvider) FindMembersByAlias(alias string) []*domain.Member { return nil }

type stubYouTubeScheduler struct{}

func (s *stubYouTubeScheduler) Start(ctx context.Context) {}
func (s *stubYouTubeScheduler) Stop()                     {}

type stubYouTubeService struct{}

func (s *stubYouTubeService) SetScraperProxyEnabled(enabled bool) bool { return enabled }
func (s *stubYouTubeService) ScraperProxyEnabled() bool                { return false }
func (s *stubYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return nil, nil
}
func (s *stubYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type stubSettingsReadWriter struct{}

func (s *stubSettingsReadWriter) Get() settings.Settings { return settings.Settings{} }
func (s *stubSettingsReadWriter) Update(newSettings settings.Settings) error {
	return nil
}

func TestBuildBotIngestionRuntimeDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotIngestionRuntimeDependencies(nil)
		if view.cache != nil || view.postgres != nil || view.irisClient != nil || view.members != nil || view.scheduler != nil || view.settings != nil {
			t.Fatal("nil deps must yield zero-value ingestion dependency view")
		}
	})

	t.Run("maps required fields only", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		postgresSvc := &database.PostgresService{}
		irisClient := &stubIrisClient{}
		membersData := &stubMemberDataProvider{}
		scheduler := &stubYouTubeScheduler{}
		settingsSvc := &stubSettingsReadWriter{}

		deps := &bot.Dependencies{
			Cache:       cacheSvc,
			Postgres:    postgresSvc,
			Client:      irisClient,
			MembersData: membersData,
			Scheduler:   scheduler,
			Settings:    settingsSvc,
		}

		view := buildBotIngestionRuntimeDependencies(deps)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
		if view.postgres != postgresSvc {
			t.Fatal("postgres mapping mismatch")
		}
		if view.irisClient != irisClient {
			t.Fatal("iris client mapping mismatch")
		}
		if view.members != membersData {
			t.Fatal("member data mapping mismatch")
		}
		if view.scheduler != scheduler {
			t.Fatal("scheduler mapping mismatch")
		}
		if view.settings != settingsSvc {
			t.Fatal("settings mapping mismatch")
		}
	})
}

func TestBuildBotWebhookRuntimeDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotWebhookRuntimeDependencies(nil)
		if view.cache != nil {
			t.Fatal("nil deps must yield zero-value webhook dependency view")
		}
	})

	t.Run("maps cache", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		deps := &bot.Dependencies{
			Cache: cacheSvc,
		}

		view := buildBotWebhookRuntimeDependencies(deps)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
	})
}

func TestBuildBotConfigSubscriberDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotConfigSubscriberDependencies(nil)
		if view.cache != nil || view.settings != nil {
			t.Fatal("nil deps must yield zero-value config subscriber view")
		}
	})

	t.Run("maps settings and cache", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		settingsSvc := &stubSettingsReadWriter{}
		deps := &bot.Dependencies{
			Cache:    cacheSvc,
			Settings: settingsSvc,
		}

		view := buildBotConfigSubscriberDependencies(deps)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
		if view.settings != settingsSvc {
			t.Fatal("settings mapping mismatch")
		}
	})
}

func TestBuildBotConfigSubscriberRuntimeDependencies(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		view := buildBotConfigSubscriberRuntimeDependencies(nil)
		if view.youtubeService != nil || view.holodexService != nil || view.alarmCRUD != nil {
			t.Fatal("nil infra must yield zero-value config subscriber runtime dependency view")
		}
	})

	t.Run("maps runtime fields", func(t *testing.T) {
		youtubeSvc := &stubYouTubeService{}
		holodexSvc := &holodex.Service{}
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}
		infra := &coreInfrastructure{
			deps: &bot.Dependencies{
				Service: youtubeSvc,
			},
			holodexService: holodexSvc,
			alarmCRUD:      alarmCRUD,
		}

		view := buildBotConfigSubscriberRuntimeDependencies(infra)
		if view.youtubeService != youtubeSvc {
			t.Fatal("youtube service mapping mismatch")
		}
		if view.holodexService != holodexSvc {
			t.Fatal("holodex service mapping mismatch")
		}
		if view.alarmCRUD != alarmCRUD {
			t.Fatal("alarm CRUD mapping mismatch")
		}
	})
}

func TestBuildBotYouTubeRuntimeDependencies(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		view := buildBotYouTubeRuntimeDependencies(nil)
		if view.sharedRateLimiter != nil || view.templateRenderer != nil || view.youtubeService != nil ||
			view.holodexService != nil || view.photoSyncService != nil {
			t.Fatal("nil infra must yield zero-value youtube runtime dependency view")
		}
	})

	t.Run("maps runtime fields", func(t *testing.T) {
		rateLimiter := &scraper.RateLimiter{}
		templateRenderer := &template.Renderer{}
		youtubeSvc := &stubYouTubeService{}
		holodexSvc := &holodex.Service{}
		photoSyncSvc := &holodex.PhotoSyncService{}
		infra := &coreInfrastructure{
			deps: &bot.Dependencies{
				Service: youtubeSvc,
			},
			sharedRL:         rateLimiter,
			templateRenderer: templateRenderer,
			holodexService:   holodexSvc,
			photoSync:        photoSyncSvc,
		}

		view := buildBotYouTubeRuntimeDependencies(infra)
		if view.sharedRateLimiter != rateLimiter {
			t.Fatal("rate limiter mapping mismatch")
		}
		if view.templateRenderer != templateRenderer {
			t.Fatal("template renderer mapping mismatch")
		}
		if view.youtubeService != youtubeSvc {
			t.Fatal("youtube service mapping mismatch")
		}
		if view.holodexService != holodexSvc {
			t.Fatal("holodex service mapping mismatch")
		}
		if view.photoSyncService != photoSyncSvc {
			t.Fatal("photo sync service mapping mismatch")
		}
	})

	t.Run("falls back to yt stack service when deps service is nil", func(t *testing.T) {
		stackService := &stubYouTubeService{}
		infra := &coreInfrastructure{
			ytStack: &providers.YouTubeStack{
				Service: stackService,
			},
		}

		view := buildBotYouTubeRuntimeDependencies(infra)
		if view.youtubeService != stackService {
			t.Fatal("youtube service must fallback to yt stack service")
		}
	})
}

func TestBuildBotAdminRuntimeDependencies(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		view := buildBotAdminRuntimeDependencies(nil)
		if view.cache != nil || view.postgres != nil || view.memberRepo != nil || view.memberCache != nil ||
			view.profiles != nil || view.alarmCRUD != nil || view.holodexService != nil || view.youtubeService != nil ||
			view.statsRepo != nil || view.activityLogger != nil || view.settings != nil || view.acl != nil || view.templateAdminSvc != nil {
			t.Fatal("nil infra must yield zero-value admin dependency view")
		}
	})

	t.Run("maps required fields only", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		postgresSvc := &database.PostgresService{}
		memberRepo := &member.Repository{}
		memberCache := &member.Cache{}
		profiles := &member.ProfileService{}
		holodexSvc := &holodex.Service{}
		youtubeSvc := &stubYouTubeService{}
		statsRepo := &stats.StatsRepository{}
		activityLogger := &activity.Logger{}
		settingsSvc := &stubSettingsReadWriter{}
		aclSvc := &acl.Service{}
		templateAdminSvc := &template.AdminService{}
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}

		infra := &coreInfrastructure{
			deps: &bot.Dependencies{
				Cache:       cacheSvc,
				Postgres:    postgresSvc,
				MemberRepo:  memberRepo,
				MemberCache: memberCache,
				Profiles:    profiles,
				Service:     youtubeSvc,
				Activity:    activityLogger,
				Settings:    settingsSvc,
				ACL:         aclSvc,
			},
			alarmCRUD:        alarmCRUD,
			holodexService:   holodexSvc,
			ytStack:          &providers.YouTubeStack{StatsRepo: statsRepo},
			templateAdminSvc: templateAdminSvc,
		}

		view := buildBotAdminRuntimeDependencies(infra)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
		if view.postgres != postgresSvc {
			t.Fatal("postgres mapping mismatch")
		}
		if view.memberRepo != memberRepo {
			t.Fatal("member repo mapping mismatch")
		}
		if view.memberCache != memberCache {
			t.Fatal("member cache mapping mismatch")
		}
		if view.profiles != profiles {
			t.Fatal("profiles mapping mismatch")
		}
		if view.alarmCRUD != alarmCRUD {
			t.Fatal("alarm CRUD mapping mismatch")
		}
		if view.holodexService != holodexSvc {
			t.Fatal("holodex service mapping mismatch")
		}
		if view.youtubeService != youtubeSvc {
			t.Fatal("youtube service mapping mismatch")
		}
		if view.statsRepo != statsRepo {
			t.Fatal("stats repo mapping mismatch")
		}
		if view.activityLogger != activityLogger {
			t.Fatal("activity logger mapping mismatch")
		}
		if view.settings != settingsSvc {
			t.Fatal("settings mapping mismatch")
		}
		if view.acl != aclSvc {
			t.Fatal("acl mapping mismatch")
		}
		if view.templateAdminSvc != templateAdminSvc {
			t.Fatal("template admin service mapping mismatch")
		}
	})
}

func TestBuildBotServerRuntimeDependencies(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		view := buildBotServerRuntimeDependencies(nil)
		if view.alarmCRUD != nil {
			t.Fatal("nil infra must yield zero-value server runtime dependency view")
		}
	})

	t.Run("maps alarm CRUD", func(t *testing.T) {
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}
		infra := &coreInfrastructure{
			alarmCRUD: alarmCRUD,
		}

		view := buildBotServerRuntimeDependencies(infra)
		if view.alarmCRUD != alarmCRUD {
			t.Fatal("alarm CRUD mapping mismatch")
		}
	})
}

func TestBuildBotRuntimeDependencyViews(t *testing.T) {
	t.Run("nil infra", func(t *testing.T) {
		views := buildBotRuntimeDependencyViews(nil)
		if views.botDeps != nil {
			t.Fatal("nil infra must yield nil bot deps")
		}
		if views.ingestion.cache != nil || views.webhook.cache != nil ||
			views.configSubscriber.cache != nil || views.configSubscriberRuntime.alarmCRUD != nil ||
			views.youtubeRuntime.templateRenderer != nil || views.adminRuntime.cache != nil ||
			views.serverRuntime.alarmCRUD != nil {
			t.Fatal("nil infra must yield zero-value runtime dependency views")
		}
	})

	t.Run("maps composed runtime views", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		postgresSvc := &database.PostgresService{}
		irisClient := &stubIrisClient{}
		membersData := &stubMemberDataProvider{}
		scheduler := &stubYouTubeScheduler{}
		settingsSvc := &stubSettingsReadWriter{}
		youtubeSvc := &stubYouTubeService{}
		holodexSvc := &holodex.Service{}
		photoSyncSvc := &holodex.PhotoSyncService{}
		rateLimiter := &scraper.RateLimiter{}
		templateRenderer := &template.Renderer{}
		templateAdminSvc := &template.AdminService{}
		var alarmCRUD domain.AlarmCRUD = testAlarmCRUD{}

		deps := &bot.Dependencies{
			Cache:       cacheSvc,
			Postgres:    postgresSvc,
			Client:      irisClient,
			MembersData: membersData,
			Scheduler:   scheduler,
			Settings:    settingsSvc,
			Service:     youtubeSvc,
		}

		infra := &coreInfrastructure{
			deps:             deps,
			alarmCRUD:        alarmCRUD,
			holodexService:   holodexSvc,
			photoSync:        photoSyncSvc,
			sharedRL:         rateLimiter,
			templateRenderer: templateRenderer,
			templateAdminSvc: templateAdminSvc,
		}

		views := buildBotRuntimeDependencyViews(infra)
		if views.botDeps != deps {
			t.Fatal("bot deps mapping mismatch")
		}
		if views.ingestion.cache != cacheSvc || views.ingestion.settings != settingsSvc {
			t.Fatal("ingestion view mapping mismatch")
		}
		if views.webhook.cache != cacheSvc {
			t.Fatal("webhook view mapping mismatch")
		}
		if views.configSubscriber.cache != cacheSvc || views.configSubscriber.settings != settingsSvc {
			t.Fatal("config subscriber view mapping mismatch")
		}
		if views.configSubscriberRuntime.alarmCRUD != alarmCRUD || views.configSubscriberRuntime.holodexService != holodexSvc {
			t.Fatal("config subscriber runtime view mapping mismatch")
		}
		if views.youtubeRuntime.sharedRateLimiter != rateLimiter || views.youtubeRuntime.templateRenderer != templateRenderer ||
			views.youtubeRuntime.youtubeService != youtubeSvc || views.youtubeRuntime.photoSyncService != photoSyncSvc {
			t.Fatal("youtube runtime view mapping mismatch")
		}
		if views.adminRuntime.cache != cacheSvc || views.adminRuntime.templateAdminSvc != templateAdminSvc {
			t.Fatal("admin runtime view mapping mismatch")
		}
		if views.serverRuntime.alarmCRUD != alarmCRUD {
			t.Fatal("server runtime view mapping mismatch")
		}
	})
}

var _ member.DataProvider = (*stubMemberDataProvider)(nil)
var _ youtube.Scheduler = (*stubYouTubeScheduler)(nil)
var _ youtube.Service = (*stubYouTubeService)(nil)
var _ settings.ReadWriter = (*stubSettingsReadWriter)(nil)
