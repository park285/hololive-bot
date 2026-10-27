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
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
)

func testBootstrapGuardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func TestInitBotInfrastructureRequiresProcessResources(t *testing.T) {
	t.Parallel()

	infra, err := appbootstrap.InitBotInfrastructure(canceledContext(), &apiconfig.BotPlaneConfig{}, nil, appbootstrap.PlaneDependencies{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.ErrorContains(t, err, "shared infra must not be nil")
}

func TestBuildRuntimeRequiresProcessResources(t *testing.T) {
	t.Parallel()

	runtime, err := BuildRuntime(canceledContext(), &apiconfig.BotPlaneConfig{}, nil, Dependencies{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "shared infra must not be nil")
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(t.Context(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger(), &appbootstrap.BotInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "create bot")
}
