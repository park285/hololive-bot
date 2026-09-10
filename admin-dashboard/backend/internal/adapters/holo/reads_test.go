package holo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func readFixture[T any](t *testing.T, method func(*Client, context.Context) (T, error), path, body string) T {
	t.Helper()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)

		if req.Method != http.MethodGet || req.URL.Path != path || req.URL.RawQuery != "" {
			t.Errorf("unexpected upstream request: %s %s", req.Method, req.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	response, err := method(client, t.Context())
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())

	return response
}

func otherReadFixtures(t *testing.T) map[string]any {
	t.Helper()

	alarms := readFixture(t, (*Client).GetAlarms, "/api/holo/alarms", `{"status":"ok","alarms":[{"roomId":"9007199254740993","roomName":"한글 방","channelId":"UC-fixture","memberName":"한글 멤버"}]}`)
	rooms := readFixture(t, (*Client).GetRooms, "/api/holo/rooms", `{"status":"ok","rooms":["9007199254740993"],"aclEnabled":false,"aclMode":"whitelist","private":"synthetic-private"}`)
	joined := readFixture(t, (*Client).GetJoinedRooms, "/api/holo/rooms/joined", `{"status":"ok","rooms":[{"chatId":"9007199254740993","name":"","type":"","memberCount":0}]}`)
	settings := readFixture(t, (*Client).GetSettings, "/api/holo/settings", `{"status":"ok","settings":{"alarmAdvanceMinutes":1440,"scraperProxyEnabled":true},"runtime":{"private":"synthetic-private"}}`)
	stats := readFixture(t, (*Client).GetStats, "/api/holo/stats", `{"status":"ok","members":0,"alarms":0,"rooms":0,"version":"fixture","uptime":"0s"}`)
	live := readFixture(t, func(c *Client, ctx context.Context) (StreamsResponse, error) { return c.GetLiveStreams(ctx, nil) }, "/api/holo/streams/live", `{"status":"ok","streams":[{"id":"stream-1","title":"한글 방송","status":"live","channel_id":"UC-fixture","start_actual":"2026-09-09T00:00:00Z"}],"private":"synthetic-private"}`)
	upcoming := readFixture(t, func(c *Client, ctx context.Context) (StreamsResponse, error) { return c.GetUpcomingStreams(ctx, nil) }, "/api/holo/streams/upcoming", `{"status":"ok","streams":[]}`)
	fixtures := map[string]any{"AlarmsResponse": []AlarmsResponse{alarms}, "RoomsResponse": []RoomsResponse{rooms}, "JoinedRoomsResponse": []JoinedRoomsResponse{joined}, "SettingsResponse": []SettingsResponse{settings}, "StatsResponse": []StatsResponse{stats}, "StreamsResponse": []StreamsResponse{live, upcoming}}

	const (
		overview = `{"channelCount":1,"detectedPostCount":0,"alarmSentPostCount":0,"successPostCount":0,"failedPostCount":0,"detectedUnsentPostCount":0,"pendingPostCount":0,"latencyMeasuredPostCount":0,"withinTargetPostCount":0,"exceededPostCount":0,"communityDetectedPostCount":0,"shortsDetectedPostCount":0,"communityExceededPostCount":0,"shortsExceededPostCount":0}`
		channel  = `{"channelId":"UC-fixture","detectedPostCount":0,"alarmSentPostCount":0,"successPostCount":0,"failedPostCount":0,"detectedUnsentPostCount":0,"pendingPostCount":0,"latencyMeasuredPostCount":0,"withinTargetPostCount":0,"exceededPostCount":0,"communityPostCount":0,"shortsPostCount":0}`
		ops      = `{"status":"ok","generatedAt":"2026-09-09T00:00:00Z","windowStart":"2026-09-08T00:00:00Z","windowEnd":"2026-09-09T00:00:00Z","windowHours":24,"observedAtBasis":"COALESCE(actual_published_at, detected_at)","slaThresholdMillis":120000,"overview":` + overview + `,"channels":[` + channel + `]}`
	)

	youtube := readFixture(t, (*Client).GetYouTubeCommunityShortsOps, "/api/holo/stats/youtube/community-shorts", ops)

	fixtures["YouTubeCommunityShortsOpsResponse"] = []YouTubeCommunityShortsOpsResponse{youtube}

	data, err := jsonv2.Marshal(fixtures)
	require.NoError(t, err)
	require.NotContains(t, string(data), "synthetic-private")
	require.NotContains(t, string(data), "scraperProxyEnabled")

	return fixtures
}

func TestOwnedReadsRejectAbsentScalarAndNullEntries(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Client) error
		body string
	}{
		{"alarm missing name", func(c *Client) error { _, err := c.GetAlarms(t.Context()); return err }, `{"status":"ok","alarms":[{"roomId":"1","channelId":"UC"}]}`},
		{"rooms null entry", func(c *Client) error { _, err := c.GetRooms(t.Context()); return err }, `{"status":"ok","rooms":[null],"aclEnabled":true,"aclMode":"whitelist"}`},
		{"rooms missing false", func(c *Client) error { _, err := c.GetRooms(t.Context()); return err }, `{"status":"ok","rooms":[],"aclMode":"whitelist"}`},
		{"joined missing zero", func(c *Client) error { _, err := c.GetJoinedRooms(t.Context()); return err }, `{"status":"ok","rooms":[{"chatId":"1","name":"","type":""}]}`},
		{"settings missing zero", func(c *Client) error { _, err := c.GetSettings(t.Context()); return err }, `{"status":"ok","settings":{}}`},
		{"settings string", func(c *Client) error { _, err := c.GetSettings(t.Context()); return err }, `{"status":"ok","settings":{"alarmAdvanceMinutes":"15"}}`},
		{"stats missing zero", func(c *Client) error { _, err := c.GetStats(t.Context()); return err }, `{"status":"ok","version":"v","uptime":"0s"}`},
		{"stream null entry", func(c *Client) error { _, err := c.GetLiveStreams(t.Context(), nil); return err }, `{"status":"ok","streams":[null]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, tc.run(memberFixtureClient(t, tc.body))) })
	}
}

func TestStreamOrgPresenceAndSupportedValues(t *testing.T) {
	query, err := streamQuery(nil)
	require.NoError(t, err)
	require.Empty(t, query)

	for _, value := range []string{"hololive", "VSpo!", "Stellive", "Independents", "all", "holo", "indie"} {
		query, err := streamQuery(&value)
		require.NoError(t, err)
		require.Equal(t, value, query.Get("org"))
	}

	for _, value := range []string{"", " ", "unsupported"} {
		_, err := streamQuery(&value)
		require.Error(t, err)
	}
}
