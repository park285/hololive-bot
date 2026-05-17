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

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func TestTwitchCheckerCheck_LiveAndOffline(t *testing.T) {
	cacheSvc := newCheckerTestCacheClient(t)
	ctx := t.Context()

	require.NoError(t, cacheSvc.HSet(ctx, notification.TwitchLoginMapKey, "aqua", "yt-1"))

	_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"yt-1", []string{"room-1"})
	require.NoError(t, err)

	startedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600,"token_type":"bearer"}`))
		case "/helix/streams":
			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte(`{"data":[{"id":"stream-1","user_id":"user-1","user_login":"aqua","user_name":"Aqua","type":"live","title":"live","viewer_count":100,"started_at":"` + startedAt + `"},{"id":"stream-2","user_id":"user-2","user_login":"aqua","user_name":"Aqua","type":"","started_at":"` + startedAt + `"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	originalBaseURL := sharedconstants.TwitchConfig.BaseURL
	originalAuthURL := sharedconstants.TwitchConfig.AuthURL

	sharedconstants.TwitchConfig.BaseURL = server.URL + "/helix"
	sharedconstants.TwitchConfig.AuthURL = server.URL + "/oauth2/token"

	t.Cleanup(func() {
		sharedconstants.TwitchConfig.BaseURL = originalBaseURL
		sharedconstants.TwitchConfig.AuthURL = originalAuthURL
	})

	checker, err := NewTwitchChecker(
		cacheSvc,
		twitch.NewClient(twitch.ClientConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		}, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	require.NoError(t, err)

	notifications, checkErr := checker.Check(ctx)
	require.NoError(t, checkErr)
	require.Len(t, notifications, 1)
	assert.Equal(t, "room-1", notifications[0].RoomID)
	assert.True(t, notifications[0].Stream.IsTwitchOnly)

	// dedup claim은 Notifier 책임이므로 checker는 동일 라이브 후보를 다시 반환한다.
	second, secondErr := checker.Check(ctx)
	require.NoError(t, secondErr)
	require.Len(t, second, 1)
	assert.Equal(t, notifications[0].Stream.ID, second[0].Stream.ID)
}

func TestTwitchCheckerCheck_APIErrors(t *testing.T) {
	t.Run("client not configured", func(t *testing.T) {
		t.Parallel()

		cacheSvc := newCheckerTestCacheClient(t)
		ctx := t.Context()
		require.NoError(t, cacheSvc.HSet(ctx, notification.TwitchLoginMapKey, "aqua", "yt-1"))

		_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"yt-1", []string{"room-1"})
		require.NoError(t, err)

		checker, err := NewTwitchChecker(cacheSvc, twitch.NewClient(twitch.ClientConfig{}, newCheckerTestLogger()), newCheckerTestLogger())
		require.NoError(t, err)

		notifications, checkErr := checker.Check(ctx)
		require.Error(t, checkErr)
		assert.Contains(t, checkErr.Error(), "get streams batch")
		assert.Nil(t, notifications)
	})

	t.Run("server 5xx", func(t *testing.T) {
		cacheSvc := newCheckerTestCacheClient(t)
		ctx := t.Context()
		require.NoError(t, cacheSvc.HSet(ctx, notification.TwitchLoginMapKey, "aqua", "yt-1"))

		_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"yt-1", []string{"room-1"})
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth2/token":
				w.WriteHeader(http.StatusOK)

				_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600,"token_type":"bearer"}`))
			case "/helix/streams":
				w.WriteHeader(http.StatusInternalServerError)

				_, _ = w.Write([]byte(`{"error":"server error"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		originalBaseURL := sharedconstants.TwitchConfig.BaseURL
		originalAuthURL := sharedconstants.TwitchConfig.AuthURL

		sharedconstants.TwitchConfig.BaseURL = server.URL + "/helix"
		sharedconstants.TwitchConfig.AuthURL = server.URL + "/oauth2/token"

		t.Cleanup(func() {
			sharedconstants.TwitchConfig.BaseURL = originalBaseURL
			sharedconstants.TwitchConfig.AuthURL = originalAuthURL
		})

		checker, err := NewTwitchChecker(
			cacheSvc,
			twitch.NewClient(twitch.ClientConfig{
				ClientID:     "client-id",
				ClientSecret: "client-secret",
			}, newCheckerTestLogger()),
			newCheckerTestLogger(),
		)
		require.NoError(t, err)

		notifications, checkErr := checker.Check(ctx)
		require.Error(t, checkErr)
		assert.Contains(t, checkErr.Error(), "get streams batch")
		assert.Nil(t, notifications)
	})
}

func TestTwitchCheckerBuildLiveNotifications_TableDriven(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name            string
		loginMappings   map[string]string
		subscriberMap   map[string][]string
		streamsResponse *twitch.StreamsResponse
		wantLen         int
	}{
		{
			name:          "stream found면 room 수만큼 알림 생성",
			loginMappings: map[string]string{"aqua": "yt-1"},
			subscriberMap: map[string][]string{"yt-1": {"room-1", "room-2"}},
			streamsResponse: &twitch.StreamsResponse{
				Data: []twitch.StreamData{
					{ID: "stream-1", UserID: "user-1", UserLogin: "aqua", UserName: "Aqua", Type: "live", Title: "hello", StartedAt: now},
				},
			},
			wantLen: 2,
		},
		{
			name:          "stream not found(매핑 없음)이면 스킵",
			loginMappings: map[string]string{"aqua": "yt-1"},
			subscriberMap: map[string][]string{"yt-1": {"room-1"}},
			streamsResponse: &twitch.StreamsResponse{
				Data: []twitch.StreamData{
					{ID: "stream-1", UserID: "user-1", UserLogin: "unknown", Type: "live", StartedAt: now},
				},
			},
			wantLen: 0,
		},
		{
			name:          "checker는 dedup 선점 없이 live 후보를 생성",
			loginMappings: map[string]string{"aqua": "yt-1"},
			subscriberMap: map[string][]string{"yt-1": {"room-1"}},
			streamsResponse: &twitch.StreamsResponse{
				Data: []twitch.StreamData{
					{ID: "stream-1", UserID: "user-1", UserLogin: "aqua", Type: "live", StartedAt: now},
				},
			},
			wantLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			setNXInvokes := 0
			cacheSvc := &cachemocks.Client{
				SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					setNXInvokes++
					return false, errors.New("checker must not preclaim dedup")
				},
			}
			checker := &TwitchChecker{
				cacheSvc: cacheSvc,
				logger:   newCheckerTestLogger(),
			}

			notifications, err := checker.buildLiveNotifications(
				t.Context(),
				tc.loginMappings,
				tc.subscriberMap,
				map[string]string{"ch1": "아쿠아"},
				tc.streamsResponse,
			)
			require.NoError(t, err)
			require.Len(t, notifications, tc.wantLen)
			assert.Equal(t, 0, setNXInvokes, "checker must not preclaim dedup before queue publish")
		})
	}
}
