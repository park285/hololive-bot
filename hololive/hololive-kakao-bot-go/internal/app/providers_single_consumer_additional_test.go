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

package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestSingleConsumerProviders_Smoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("stream clients", func(t *testing.T) {
		chzzkClient := ProvideChzzkClient(http.DefaultClient, config.ChzzkConfig{
			ClientID:     "cid",
			ClientSecret: "sec",
		}, logger)
		require.NotNil(t, chzzkClient)

		twitchClient := ProvideTwitchClient(config.TwitchConfig{
			ClientID:     "tid",
			ClientSecret: "tsec",
		}, logger)
		require.NotNil(t, twitchClient)
	})

	t.Run("alarm repository and worker pool", func(t *testing.T) {
		repo := ProvideAlarmRepository(&dbmocks.Client{}, logger)
		require.NotNil(t, repo)

		pool, err := ProvideAlarmWorkerPool()
		require.NoError(t, err)
		require.NotNil(t, pool)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, pool.ShutdownWait(ctx))
	})

	t.Run("alarm service", func(t *testing.T) {
		t.Cleanup(func() {
			_ = notification.CloseAllAlarmServices(context.Background())
		})

		svc, err := ProvideAlarmService(
			[]int{10, 3},
			&cachemocks.Client{},
			nil,
			nil,
			nil,
			&stubMemberDataProvider{},
			&alarm.Repository{},
			logger,
		)
		require.NoError(t, err)
		require.NotNil(t, svc)
		assert.Equal(t, []int{10, 3, 1}, svc.GetTargetMinutes())
	})

	t.Run("member matcher", func(t *testing.T) {
		matcher := ProvideMemberMatcher(context.Background(), &stubMemberDataProvider{}, &cachemocks.Client{}, nil, logger)
		require.NotNil(t, matcher)
	})

	t.Run("fetch profiles helpers", func(t *testing.T) {
		fetchLogger, cleanup, err := ProvideFetchProfilesLogger()
		require.NoError(t, err)
		require.NotNil(t, fetchLogger)
		require.NotNil(t, cleanup)
		cleanup()

		client := ProvideFetchProfilesHTTPClient()
		require.NotNil(t, client)
		assert.Equal(t, constants.OfficialProfileConfig.RequestTimeout, client.Timeout)
	})
}
