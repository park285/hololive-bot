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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func testBootstrapGuardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func TestInitializeBotDependencies_ContextCanceled(t *testing.T) {
	t.Parallel()

	deps, cleanup, err := InitializeBotDependencies(canceledContext(), &configsettings.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitializeBotRuntime_ContextCanceled(t *testing.T) {
	t.Parallel()

	runtime, cleanup, err := InitializeBotRuntime(canceledContext(), &configsettings.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitInfraResources_ContextCanceled(t *testing.T) {
	t.Parallel()

	resources, err := appbootstrap.InitInfraResources(canceledContext(), &configsettings.Config{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, resources)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestProvideTriggerHandler_ReturnsHandler(t *testing.T) {
	t.Parallel()

	handler := sharedserver.NewTriggerHandler(nil, nil, nil, testBootstrapGuardLogger())
	require.NotNil(t, handler)
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(t.Context(), &configsettings.Config{}, testBootstrapGuardLogger(), &appbootstrap.BotInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestResolveLLMSchedulerClients_Guards(t *testing.T) {
	t.Parallel()

	major, news, err := appbootstrap.ResolveLLMSchedulerClients(&configsettings.Config{}, testBootstrapGuardLogger())
	require.NoError(t, err)
	assert.Nil(t, major)
	assert.Nil(t, news)

	major, news, err = appbootstrap.ResolveLLMSchedulerClients(&configsettings.Config{
		LLMSchedulerURL: "http://localhost:18080",
		Server:          configsettings.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	require.NoError(t, err)
	assert.NotNil(t, major)
	assert.NotNil(t, news)
}

// https LLM scheduler URL이 설정됐는데 내부 H3 env가 없으면 명령을 조용히 끄거나 TCP client로 내려가지 않고
// 기동 오류다(stack audit 2026-09-26).
func TestResolveLLMSchedulerClientsFailsWithoutInternalH3Env(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", "")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "")

	major, news, err := appbootstrap.ResolveLLMSchedulerClients(&configsettings.Config{
		LLMSchedulerURL: "https://127.0.0.1:30003",
		Server:          configsettings.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HOLOLIVE_INTERNAL_H3_CA_CERT_FILE")
	assert.Nil(t, major)
	assert.Nil(t, news)
}

func TestBuildBotDependencyModules_MapsInputs(t *testing.T) {
	t.Parallel()

	logger := testBootstrapGuardLogger()
	cacheService := &cache.Service{}
	postgresService := &database.PostgresService{}
	memberRepository := &member.Repository{}
	memberCache := &member.Cache{}
	memberData := &stubMemberDataProvider{}
	matcherService := &matcher.Matcher{}
	ytStack := &providers.YouTubeStack{}
	activityLogger := &activity.Logger{}
	settingsService := &settings.Service{}
	aclService := &acl.Service{}
	commandBuilder := orchcmd.CommandBuilder(func(_ *handlercore.Dependencies) handlercore.Command { return nil })

	modules := buildBotDependencyModules(
		&configsettings.Config{
			Bot:          configsettings.BotConfig{SelfUser: "self-user"},
			Iris:         configsettings.IrisConfig{BaseURL: "https://iris.example"},
			Notification: configsettings.NotificationConfig{AdvanceMinutes: []int{5}},
		},
		&sharedmodules.InfraModule{Cache: cacheService, Postgres: postgresService, MemberRepository: memberRepository, MemberCache: memberCache},
		&appbootstrap.ScraperHolodexFoundation{
			HolodexService: &holodexprovider.Service{},
		},
		&appbootstrap.AlarmYouTubeStackComponents{
			AlarmMode:       &appbootstrap.AlarmModeComponents{AlarmCRUD: testAlarmCRUD{}, MemberDataSource: memberData},
			Matcher:         matcherService,
			YouTubeStack:    ytStack,
			ActivityLogger:  activityLogger,
			SettingsService: settingsService,
		},
		&appbootstrap.CoreIntegrationServices{
			ACLService:           aclService,
			MajorEventRepository: &stubMajorEventRepository{},
			MemberNewsService:    &stubMemberNewsService{},
			CommandBuilders:      []orchcmd.CommandBuilder{commandBuilder},
		},
		&messaging.MessageAdapter{},
		&formatter.ResponseFormatter{},
		nil,
		&stubIrisClient{},
		logger,
	)

	assert.Equal(t, "self-user", modules.Core.BotSelfUser)
	assert.Equal(t, "https://iris.example", modules.Core.IrisBaseURL)
	assert.Same(t, cacheService, modules.Data.Cache)
	assert.Same(t, postgresService, modules.Data.Postgres)
	assert.Same(t, memberRepository, modules.Data.MemberRepository)
	assert.Same(t, memberCache, modules.Data.MemberCache)
	assert.Same(t, memberData, modules.Data.MembersData)
	assert.Same(t, matcherService, modules.Stream.MemberMatch)
	assert.Same(t, ytStack, modules.Stream.YTStack)
	assert.Same(t, activityLogger, modules.Support.ActivityLogger)
	assert.Same(t, settingsService, modules.Support.Settings)
	assert.Same(t, aclService, modules.Support.ACL)
	require.Len(t, modules.Feature.CommandBuilders, 1)
	assert.NotNil(t, modules.Feature.CommandBuilders[0])
}
