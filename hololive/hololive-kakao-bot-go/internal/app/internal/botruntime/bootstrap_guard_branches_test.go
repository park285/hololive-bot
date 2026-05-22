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

package botruntime

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

type stubWebhookMessageHandler struct{}

func (stubWebhookMessageHandler) HandleMessage(context.Context, *iris.Message) {}

func testBootstrapGuardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func closeAlarmServices(t *testing.T) {
	t.Helper()

	// t.Cleanup 시점에 t.Context()는 이미 canceled 상태이므로 독립 context 사용
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, notification.CloseAllAlarmServices(ctx))
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

	resources, err := appbootstrap.InitInfraResources(canceledContext(), &config.Config{}, testBootstrapGuardLogger())
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

	handler := sharedserver.NewTriggerHandler(nil, nil, nil, testBootstrapGuardLogger())
	require.NotNil(t, handler)
}

func TestBuildBotWebhookHandler_ReturnsClosableHandler(t *testing.T) {
	t.Setenv("IRIS_WEBHOOK_TOKEN", "token")

	cfg := &config.Config{
		Iris: config.IrisConfig{WebhookToken: "token"},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      1,
			EnqueueTimeout: 1,
			HandlerTimeout: 1,
		},
	}

	handler, err := appbootstrap.BuildBotWebhookHandler(
		cfg,
		stubWebhookMessageHandler{},
		botWebhookRuntimeDependencies{Cache: &cache.Service{}},
		testBootstrapGuardLogger(),
	)
	require.NoError(t, err)
	require.NotNil(t, handler)

	options := reflect.ValueOf(handler).Elem().FieldByName("options")
	require.True(t, options.IsValid(), "reflect: field 'options' not found on Handler")
	assert.Equal(t, int64(cfg.Webhook.WorkerCount), options.FieldByName("WorkerCount").Int())
	assert.Equal(t, int64(cfg.Webhook.QueueSize), options.FieldByName("QueueSize").Int())
	assert.Equal(t, int64(cfg.Webhook.EnqueueTimeout), options.FieldByName("EnqueueTimeout").Int())
	assert.Equal(t, int64(cfg.Webhook.HandlerTimeout), options.FieldByName("HandlerTimeout").Int())
	assert.Equal(t, cfg.Webhook.RequireHTTP2, options.FieldByName("RequireHTTP2").Bool())

	dedupField := reflect.ValueOf(handler).Elem().FieldByName("dedup")
	require.True(t, dedupField.IsValid(), "reflect: field 'dedup' not found on Handler")
	require.False(t, dedupField.IsNil(), "dedup must not be nil")
	dedupType := dedupField.Elem().Type().String()
	assert.True(t, strings.Contains(dedupType, "ValkeyDeduplicator"), "dedup type = %s", dedupType)
	require.NoError(t, handler.Close())
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(t.Context(), &config.Config{}, testBootstrapGuardLogger(), &appbootstrap.BotInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestResolveLLMSchedulerClients_Guards(t *testing.T) {
	t.Parallel()

	major, news := appbootstrap.ResolveLLMSchedulerClients(&config.Config{}, testBootstrapGuardLogger())
	assert.Nil(t, major)
	assert.Nil(t, news)

	major, news = appbootstrap.ResolveLLMSchedulerClients(&config.Config{
		LLMSchedulerURL: "http://localhost:18080",
		Server:          config.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	assert.NotNil(t, major)
	assert.NotNil(t, news)
}

func TestBuildBotDependencyModules_MapsInputs(t *testing.T) {
	t.Parallel()

	logger := testBootstrapGuardLogger()
	cache := &cache.Service{}
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
	commandBuilder := bot.CommandBuilder(func(_ *command.Dependencies) command.Command { return nil })

	modules := buildBotDependencyModules(
		&config.Config{
			Bot:          config.BotConfig{SelfUser: "self-user"},
			Iris:         config.IrisConfig{BaseURL: "https://iris.example"},
			Notification: config.NotificationConfig{AdvanceMinutes: []int{5}},
		},
		&sharedmodules.InfraModule{Cache: cache, Postgres: postgresSvc, MemberRepo: memberRepo, MemberCache: memberCache},
		&appbootstrap.AlarmModeComponents{AlarmCRUD: testAlarmCRUD{}, ChzzkClient: chzzkClient, TwitchClient: twitchClient, MemberDataSource: memberData},
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
		[]bot.CommandBuilder{commandBuilder},
		workerPool,
		logger,
	)

	assert.Equal(t, "self-user", modules.Core.BotSelfUser)
	assert.Equal(t, "https://iris.example", modules.Core.IrisBaseURL)
	assert.Same(t, cache, modules.Data.Cache)
	assert.Same(t, postgresSvc, modules.Data.Postgres)
	assert.Same(t, memberRepo, modules.Data.MemberRepo)
	assert.Same(t, memberCache, modules.Data.MemberCache)
	assert.Same(t, memberData, modules.Data.MembersData)
	assert.Same(t, chzzkClient, modules.Stream.ChzzkClient)
	assert.Same(t, twitchClient, modules.Stream.TwitchClient)
	assert.Same(t, matcherSvc, modules.Stream.MemberMatch)
	assert.Same(t, ytStack, modules.Stream.YTStack)
	assert.Same(t, activityLogger, modules.Support.ActivityLogger)
	assert.Same(t, settingsSvc, modules.Support.SettingsSvc)
	assert.Same(t, aclSvc, modules.Support.ACLSvc)
	require.Len(t, modules.Feature.CommandBuilders, 1)
	assert.NotNil(t, modules.Feature.CommandBuilders[0])
	assert.Same(t, workerPool, modules.Support.WorkerPool)
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
	assert.Same(t, memberData, deps.MemberDataProvider)
	assert.NotNil(t, deps.ChzzkClient)
	assert.NotNil(t, deps.TwitchClient)
	assert.NotNil(t, deps.AlarmService)
}

func TestInitAlarmModeComponents_SuccessWithNilRepository(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() { closeAlarmServices(t) })

	memberData := &stubMemberDataProvider{}
	components, err := initAlarmModeComponents(
		t.Context(),
		&config.Config{
			Notification: config.NotificationConfig{AdvanceMinutes: []int{5}},
			Scraper:      config.ScraperConfig{},
		},
		&sharedmodules.InfraModule{},
		&holodex.Service{},
		memberData,
		nil,
		testBootstrapGuardLogger(),
	)
	require.NoError(t, err)
	require.NotNil(t, components)
	assert.Same(t, memberData, components.MemberDataSource)
	assert.NotNil(t, components.AlarmService)
	assert.NotNil(t, components.ChzzkClient)
	assert.NotNil(t, components.TwitchClient)
}
