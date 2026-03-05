package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

type stubWebhookMessageHandler struct{}

func (stubWebhookMessageHandler) HandleMessage(context.Context, *iris.Message) {}

type stubPollerPostgres struct {
	db *gorm.DB
}

func (s *stubPollerPostgres) GetPool() *pgxpool.Pool     { return nil }
func (s *stubPollerPostgres) GetGormDB() *gorm.DB        { return s.db }
func (s *stubPollerPostgres) Ping(context.Context) error { return nil }
func (s *stubPollerPostgres) Close() error               { return nil }
func testBootstrapGuardLogger() *slog.Logger             { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
func closeAlarmServices(t *testing.T) {
	t.Helper()
	require.NoError(t, notification.CloseAllAlarmServices(context.Background()))
}
func TestInitializeBotDependencies_ContextCanceled(t *testing.T) {
	t.Parallel()

	deps, cleanup, err := InitializeBotDependencies(canceledContext(), &config.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitializeBotRuntime_ContextCanceled(t *testing.T) {
	t.Parallel()

	runtime, cleanup, err := InitializeBotRuntime(canceledContext(), &config.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitInfraResources_ContextCanceled(t *testing.T) {
	t.Parallel()

	resources, err := initInfraResources(canceledContext(), &config.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, resources)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitializeWarmMemberCache_ContextCanceled(t *testing.T) {
	t.Parallel()

	memberCache, cleanup, err := InitializeWarmMemberCache(canceledContext(), &config.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, memberCache)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide database resources")
}

func TestInitializeDBIntegrationRuntime_ContextCanceled(t *testing.T) {
	t.Parallel()

	runtime, cleanup, err := InitializeDBIntegrationRuntime(canceledContext(), config.PostgresConfig{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide database resources")
}

func TestProvideTriggerHandler_ReturnsHandler(t *testing.T) {
	t.Parallel()

	handler := ProvideTriggerHandler(nil, nil, nil, testBootstrapGuardLogger())
	require.NotNil(t, handler)
}

func TestBuildBotWebhookHandler_ReturnsClosableHandler(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Iris: config.IrisConfig{WebhookToken: "token"},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      1,
			EnqueueTimeout: 1,
			HandlerTimeout: 1,
		},
	}

	handler := buildBotWebhookHandler(
		cfg,
		stubWebhookMessageHandler{},
		botWebhookRuntimeDependencies{cache: &cache.Service{}},
		testBootstrapGuardLogger(),
	)
	require.NotNil(t, handler)
	require.NoError(t, handler.Close())
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(context.Background(), &config.Config{}, testBootstrapGuardLogger(), &coreInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestBuildBotAdminServerDependencies_GuardBranches(t *testing.T) {
	t.Parallel()

	logger := testBootstrapGuardLogger()

	deps, err := buildBotAdminServerDependencies(context.Background(), nil, botAdminRuntimeDependencies{}, nil, logger)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "config is nil")

	deps, err = buildBotAdminServerDependencies(context.Background(), &config.Config{}, botAdminRuntimeDependencies{}, nil, logger)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "admin dependency view is incomplete")
}

func TestResolveLLMSchedulerClients_Guards(t *testing.T) {
	t.Parallel()

	major, news := resolveLLMSchedulerClients(&config.Config{}, testBootstrapGuardLogger())
	assert.Nil(t, major)
	assert.Nil(t, news)

	major, news = resolveLLMSchedulerClients(&config.Config{
		LLMSchedulerURL: "http://localhost:18080",
		Server:          config.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	assert.NotNil(t, major)
	assert.NotNil(t, news)
}

func TestBuildBotDependencyModules_MapsInputs(t *testing.T) {
	t.Parallel()

	logger := testBootstrapGuardLogger()
	cacheSvc := &cache.Service{}
	postgresSvc := &database.PostgresService{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	memberData := &stubMemberDataProvider{}
	chzzkClient := &chzzk.Client{}
	twitchClient := &twitch.Client{}
	matcherSvc := &matcher.MemberMatcher{}
	ytStack := &providers.YouTubeStack{}
	activityLogger := &activity.Logger{}
	settingsSvc := &settings.Service{}
	aclSvc := &acl.Service{}
	workerPool := &workerpool.Pool{}

	modules := buildBotDependencyModules(
		&config.Config{
			Bot:          config.BotConfig{SelfUser: "self-user"},
			Iris:         config.IrisConfig{BaseURL: "https://iris.example"},
			Notification: config.NotificationConfig{AdvanceMinutes: []int{5}},
		},
		&infraResources{cacheService: cacheSvc, postgresService: postgresSvc, memberRepo: memberRepo, memberCache: memberCache},
		&alarmModeComponents{alarmCRUD: testAlarmCRUD{}, chzzkClient: chzzkClient, twitchClient: twitchClient, memberDataSource: memberData},
		&holodex.Service{},
		&adapter.MessageAdapter{},
		&adapter.ResponseFormatter{},
		&stubIrisClient{},
		&member.ProfileService{},
		matcherSvc,
		ytStack,
		activityLogger,
		settingsSvc,
		aclSvc,
		&stubMajorEventRepo{},
		&stubMemberNewsService{},
		workerPool,
		logger,
	)

	assert.Equal(t, "self-user", modules.core.botSelfUser)
	assert.Equal(t, "https://iris.example", modules.core.irisBaseURL)
	assert.Same(t, cacheSvc, modules.data.cacheSvc)
	assert.Same(t, postgresSvc, modules.data.postgres)
	assert.Same(t, memberRepo, modules.data.memberRepo)
	assert.Same(t, memberCache, modules.data.memberCache)
	assert.Same(t, memberData, modules.data.membersData)
	assert.Same(t, chzzkClient, modules.stream.chzzkClient)
	assert.Same(t, twitchClient, modules.stream.twitchClient)
	assert.Same(t, matcherSvc, modules.stream.memberMatch)
	assert.Same(t, ytStack, modules.stream.ytStack)
	assert.Same(t, activityLogger, modules.support.activityLogger)
	assert.Same(t, settingsSvc, modules.support.settingsSvc)
	assert.Same(t, aclSvc, modules.support.aclSvc)
	assert.Same(t, workerPool, modules.support.workerPool)
}

func TestInitAlarmDependencies_SuccessWithMinimalInputs(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() { closeAlarmServices(t) })

	memberData := &stubMemberDataProvider{}
	deps, err := initAlarmDependencies(
		config.ChzzkConfig{},
		config.TwitchConfig{},
		[]int{5},
		false,
		nil,
		nil,
		memberData,
		nil,
		testBootstrapGuardLogger(),
	)
	require.NoError(t, err)
	require.NotNil(t, deps)
	assert.Same(t, memberData, deps.memberDataProvider)
	assert.NotNil(t, deps.chzzkClient)
	assert.NotNil(t, deps.twitchClient)
	assert.NotNil(t, deps.alarmService)
}

func TestInitAlarmModeComponents_SuccessWithNilRepository(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() { closeAlarmServices(t) })

	memberData := &stubMemberDataProvider{}
	components, err := initAlarmModeComponents(
		context.Background(),
		&config.Config{
			Notification: config.NotificationConfig{AdvanceMinutes: []int{5}},
			Scraper:      config.ScraperConfig{},
		},
		&infraResources{},
		&holodex.Service{},
		memberData,
		nil,
		testBootstrapGuardLogger(),
	)
	require.NoError(t, err)
	require.NotNil(t, components)
	assert.Same(t, memberData, components.memberDataSource)
	assert.NotNil(t, components.alarmService)
	assert.NotNil(t, components.chzzkClient)
	assert.NotNil(t, components.twitchClient)
}

func TestBuildBotChannelPollerRegistrations_ReturnsExpectedDefaults(t *testing.T) {
	t.Parallel()

	registrations := buildBotChannelPollerRegistrations(
		&stubPollerPostgres{},
		scraper.ProxyConfig{},
		nil,
		&cache.Service{},
	)

	require.Len(t, registrations, 5)
	intervals := providers.DefaultPollerIntervals()

	assert.Equal(t, "videos", registrations[0].Poller.Name())
	assert.Equal(t, "shorts", registrations[1].Poller.Name())
	assert.Equal(t, "community", registrations[2].Poller.Name())
	assert.Equal(t, "channel_stats", registrations[3].Poller.Name())
	assert.Equal(t, "live", registrations[4].Poller.Name())

	assert.Equal(t, intervals.Videos, registrations[0].Interval)
	assert.Equal(t, intervals.Shorts, registrations[1].Interval)
	assert.Equal(t, intervals.Community, registrations[2].Interval)
	assert.Equal(t, intervals.Stats, registrations[3].Interval)
	assert.Equal(t, intervals.Live, registrations[4].Interval)
}
