package checker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

func TestTwitchCheckerCheck_LiveAndOffline(t *testing.T) {
	cacheSvc := newCheckerTestCacheClient(t)
	ctx := context.Background()

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

	// 동일 stream_id 재조회 시 dedup으로 스킵
	second, secondErr := checker.Check(ctx)
	require.NoError(t, secondErr)
	assert.Empty(t, second)
}

func TestTwitchCheckerCheck_APIErrors(t *testing.T) {
	t.Run("client not configured", func(t *testing.T) {
		t.Parallel()

		cacheSvc := newCheckerTestCacheClient(t)
		ctx := context.Background()
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
		ctx := context.Background()
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
		name             string
		loginMappings    map[string]string
		subscriberMap    map[string][]string
		streamsResponse  *twitch.StreamsResponse
		setNXClaimed     bool
		setNXErr         error
		wantLen          int
		wantErrContains  string
		wantSetNXInvokes int
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
			setNXClaimed:     true,
			wantLen:          2,
			wantSetNXInvokes: 1,
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
			setNXClaimed:     true,
			wantLen:          0,
			wantSetNXInvokes: 0,
		},
		{
			name:          "dedup 충돌이면 알림 생성 안 함",
			loginMappings: map[string]string{"aqua": "yt-1"},
			subscriberMap: map[string][]string{"yt-1": {"room-1"}},
			streamsResponse: &twitch.StreamsResponse{
				Data: []twitch.StreamData{
					{ID: "stream-1", UserID: "user-1", UserLogin: "aqua", Type: "live", StartedAt: now},
				},
			},
			setNXClaimed:     false,
			wantLen:          0,
			wantSetNXInvokes: 1,
		},
		{
			name:          "dedup 에러면 실패 반환",
			loginMappings: map[string]string{"aqua": "yt-1"},
			subscriberMap: map[string][]string{"yt-1": {"room-1"}},
			streamsResponse: &twitch.StreamsResponse{
				Data: []twitch.StreamData{
					{ID: "stream-1", UserID: "user-1", UserLogin: "aqua", Type: "live", StartedAt: now},
				},
			},
			setNXErr:         errors.New("setnx failed"),
			wantErrContains:  "claim dedup key",
			wantSetNXInvokes: 1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			setNXInvokes := 0
			cacheSvc := &cachemocks.Client{
				SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					setNXInvokes++
					if tc.setNXErr != nil {
						return false, tc.setNXErr
					}
					return tc.setNXClaimed, nil
				},
			}
			checker := &TwitchChecker{
				cacheSvc: cacheSvc,
				logger:   newCheckerTestLogger(),
			}

			notifications, err := checker.buildLiveNotifications(
				context.Background(),
				tc.loginMappings,
				tc.subscriberMap,
				tc.streamsResponse,
			)
			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				assert.Nil(t, notifications)
			} else {
				require.NoError(t, err)
				require.Len(t, notifications, tc.wantLen)
			}
			assert.Equal(t, tc.wantSetNXInvokes, setNXInvokes)
		})
	}
}
