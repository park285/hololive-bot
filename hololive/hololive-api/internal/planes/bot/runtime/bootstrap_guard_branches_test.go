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

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apiserver "github.com/kapu/hololive-api/internal/httpapi"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
)

func testBootstrapGuardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func TestInitBotInfrastructureContextCanceled(t *testing.T) {
	t.Parallel()

	infra, err := appbootstrap.InitBotInfrastructure(canceledContext(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.ErrorContains(t, err, "provide infra resources")
}

func TestBuildRuntimeContextCanceled(t *testing.T) {
	t.Parallel()

	runtime, err := BuildRuntime(canceledContext(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestInitInfraResources_ContextCanceled(t *testing.T) {
	t.Parallel()

	resources, err := appbootstrap.InitInfraResources(canceledContext(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, resources)
	assert.Contains(t, err.Error(), "provide infra resources")
}

func TestProvideTriggerHandler_ReturnsHandler(t *testing.T) {
	t.Parallel()

	handler := apiserver.NewTriggerHandler(nil, nil, nil, testBootstrapGuardLogger())
	require.NotNil(t, handler)
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(t.Context(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger(), &appbootstrap.BotInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestResolveLLMSchedulerClients_Guards(t *testing.T) {
	t.Parallel()

	clients, err := appbootstrap.ResolveLLMSchedulerClients(&apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger())
	require.NoError(t, err)
	assert.Nil(t, clients.MajorEvent)
	assert.Nil(t, clients.MemberNews)

	clients, err = appbootstrap.ResolveLLMSchedulerClients(&apiconfig.BotPlaneConfig{
		LLMSchedulerURL: "http://localhost:18080",
		Server:          configsettings.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	require.NoError(t, err)
	assert.NotNil(t, clients.MajorEvent)
	assert.NotNil(t, clients.MemberNews)
}

// https LLM scheduler URL이 설정됐는데 내부 H3 env가 없으면 명령을 조용히 끄거나 TCP client로 내려가지 않고
// 기동 오류다(stack audit 2026-09-26).
func TestResolveLLMSchedulerClientsFailsWithoutInternalH3Options(t *testing.T) {
	clients, err := appbootstrap.ResolveLLMSchedulerClients(&apiconfig.BotPlaneConfig{
		LLMSchedulerURL: "https://127.0.0.1:30003",
		Server:          configsettings.ServerConfig{APIKey: "test-api-key"},
	}, testBootstrapGuardLogger())
	require.Error(t, err)
	require.ErrorContains(t, err, "configure major event client transport")
	assert.Nil(t, clients.MajorEvent)
	assert.Nil(t, clients.MemberNews)
}
