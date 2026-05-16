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

package checking

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

func TestChzzkCheckerCheck_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		responseBody  string
		responseDelay time.Duration
		clientTimeout time.Duration
		wantLen       int
		expectSecond  bool
		secondWantLen int
	}{
		{
			name:          "live OPEN이면 checker가 후보를 생성하고 dedup은 Notifier에 위임",
			statusCode:    http.StatusOK,
			responseBody:  `{"code":200,"content":{"status":"OPEN","liveTitle":"치지직 라이브","concurrentUserCount":77,"liveCategoryValue":"게임"}}`,
			wantLen:       2,
			expectSecond:  true,
			secondWantLen: 2,
		},
		{
			name:         "CLOSE 상태면 알림 없음",
			statusCode:   http.StatusOK,
			responseBody: `{"code":200,"content":{"status":"CLOSE"}}`,
			wantLen:      0,
		},
		{
			name:         "API 에러는 graceful skip",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"code":500}`,
			wantLen:      0,
		},
		{
			name:          "timeout 에러는 graceful skip",
			statusCode:    http.StatusOK,
			responseBody:  `{"code":200,"content":{"status":"OPEN"}}`,
			responseDelay: 80 * time.Millisecond,
			clientTimeout: 10 * time.Millisecond,
			wantLen:       0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cacheSvc := newCheckerTestCacheClient(t)
			ctx := t.Context()

			require.NoError(t, cacheSvc.HSet(ctx, notification.ChzzkChannelMapKey, "yt-1", "chzzk-1"))

			_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"yt-1", []string{"room-1", "room-2"})
			require.NoError(t, err)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.responseDelay > 0 {
					time.Sleep(tc.responseDelay)
				}

				w.WriteHeader(tc.statusCode)

				_, _ = w.Write([]byte(tc.responseBody))
			}))
			t.Cleanup(server.Close)

			httpClient := server.Client()

			if tc.clientTimeout > 0 {
				httpClient.Timeout = tc.clientTimeout
			}

			checker, err := NewChzzkChecker(
				cacheSvc,
				chzzk.NewClient(httpClient, server.URL, newCheckerTestLogger()),
				newCheckerTestLogger(),
			)
			require.NoError(t, err)

			notifications, err := checker.Check(ctx)
			require.NoError(t, err)
			require.Len(t, notifications, tc.wantLen)

			if tc.wantLen > 0 {
				assert.Equal(t, "room-1", notifications[0].RoomID)
				assert.True(t, notifications[0].Stream.IsChzzkOnly)
				assert.Equal(t, "yt-1", notifications[0].Stream.ChannelID)
			}

			if tc.expectSecond {
				second, secondErr := checker.Check(ctx)
				require.NoError(t, secondErr)
				assert.Len(t, second, tc.secondWantLen)
			}
		})
	}
}

func TestChzzkCheckerCheck_DoesNotPreclaimDedup(t *testing.T) {
	t.Parallel()

	setNXCalls := 0
	cacheSvc := &cachemocks.Client{
		HGetAllFunc: func(context.Context, string) (map[string]string, error) {
			return map[string]string{"yt-1": "chzzk-1"}, nil
		},
		SMembersFunc: func(context.Context, string) ([]string, error) {
			return []string{"room-1"}, nil
		},
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			setNXCalls++
			return false, errors.New("checker must not preclaim dedup")
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte(`{"code":200,"content":{"status":"OPEN"}}`))
	}))
	t.Cleanup(server.Close)

	checker, err := NewChzzkChecker(
		cacheSvc,
		chzzk.NewClient(server.Client(), server.URL, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	require.NoError(t, err)

	notifications, checkErr := checker.Check(t.Context())
	require.NoError(t, checkErr)
	require.Len(t, notifications, 1)
	assert.Equal(t, 0, setNXCalls, "checker must not preclaim dedup before queue publish")
}
