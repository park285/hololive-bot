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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func TestProvideTriggerHandler_ReturnsUsableHandler(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	handler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)
	require.NotNil(t, handler)

	router := gin.New()
	handler.RegisterInternalRoutesWithoutAuth(router.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, triggercontracts.MajorEventWeeklyPath, http.NoBody)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(t, http.StatusServiceUnavailable, res.Code)
}

func TestBuildBotRuntime_RejectsNilAppConfig(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := buildBotRuntime(t.Context(), nil, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "app config is nil")
}

func TestBuildBotRuntime_RejectsNilInfrastructure(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := buildBotRuntime(t.Context(), &settings.Config{}, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "infra is nil")
}
