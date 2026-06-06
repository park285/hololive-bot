package pollers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

const channelStatsAboutHTML = `<html><body><script>var ytInitialData = ` +
	`{"onResponseReceivedEndpoints":[{"showEngagementPanelEndpoint":{"engagementPanel":{"engagementPanelSectionListRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"aboutChannelRenderer":{"metadata":{"aboutChannelViewModel":{` +
	`"subscriberCountText":"2.76M subscribers",` +
	`"viewCountText":"1,056,229,686 views",` +
	`"videoCountText":"2,429 videos",` +
	`"joinedDateText":{"content":"Joined Jul 2, 2019"},` +
	`"description":"Test channel description",` +
	`"country":"Japan",` +
	`"handle":"@testchannel"` +
	`}}}}]}}]}}}}}}],"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"endpoint":{"browseEndpoint":{"canonicalBaseUrl":"/@testchannel"}}}}]}}}` +
	`;</script></body></html>`

const channelSnippetHTML = `<html><body><script>var ytInitialData = ` +
	`{"header":{"pageHeaderRenderer":{"content":{"pageHeaderViewModel":{"image":{"decoratedAvatarViewModel":{"avatar":{"avatarViewModel":{"image":{"sources":[{"url":"https://img.test/avatar.jpg","width":88,"height":88}]}}}}},"banner":{"imageBannerViewModel":{"image":{"sources":[{"url":"https://img.test/banner.jpg","width":1280,"height":351}]}}}}}}}}}` +
	`;</script></body></html>`

func newChannelStatsTestClient(t *testing.T, transport http.RoundTripper) *scraper.Client {
	t.Helper()
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{Transport: transport}),
	)
}

func bothPagesTransport(req *http.Request) (*http.Response, error) {
	switch {
	case strings.Contains(req.URL.Path, "/about"):
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(channelStatsAboutHTML)), Header: make(http.Header), Request: req}, nil
	default:
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(channelSnippetHTML)), Header: make(http.Header), Request: req}, nil
	}
}

func TestChannelStatsPollerSavesSnapshot(t *testing.T) {
	db := newPollerBatchTestDB(t)

	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(bothPagesTransport))

	poller := NewChannelStatsPoller(client, db)
	require.NoError(t, poller.Poll(context.Background(), "UC_SNAP"))

	var snapshot domain.YouTubeChannelStatsSnapshot
	require.NoError(t, db.Where("channel_id = ?", "UC_SNAP").First(&snapshot).Error)
	require.Equal(t, int64(2_760_000), snapshot.SubscriberCount)
	require.Equal(t, int64(1_056_229_686), snapshot.ViewCount)
	require.Equal(t, int64(2429), snapshot.VideoCount)
	require.Equal(t, "Japan", snapshot.Country)
}

func TestChannelStatsPollerUpdatesStaleProfile(t *testing.T) {
	db := newPollerBatchTestDB(t)

	// 프로필 행이 없는 상태 → needsUpdate = true → snippet 호출 → 프로필 생성
	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(bothPagesTransport))

	poller := NewChannelStatsPoller(client, db)
	require.NoError(t, poller.Poll(context.Background(), "UC_STALE"))

	// 프로필이 생성되었는지 확인 (avatar 컬럼 JSON 텍스트가 있음)
	var avatarJSON string
	require.NoError(t, db.QueryRow(context.Background(), "SELECT avatar::text FROM youtube_channel_profiles WHERE channel_id = $1", "UC_STALE").Scan(&avatarJSON))
	require.Contains(t, avatarJSON, "https://img.test/avatar.jpg")
}

func TestChannelStatsPollerSkipsFreshProfile(t *testing.T) {
	db := newPollerBatchTestDB(t)

	snippetCallCount := 0
	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/about"):
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(channelStatsAboutHTML)), Header: make(http.Header), Request: req}, nil
		default:
			snippetCallCount++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(channelSnippetHTML)), Header: make(http.Header), Request: req}, nil
		}
	}))

	// 현재 시각 기준 fresh 프로필을 직접 INSERT (avatar/banner는 NULL → scan 없이 updated_at만 읽어도 됨)
	// updateProfileIfStale은 profile.UpdatedAt만 비교하므로 avatar가 NULL이어도 동작한다.
	freshTime := time.Now().Add(-1 * time.Hour).UTC()
	_, err := db.Exec(
		context.Background(),
		"INSERT INTO youtube_channel_profiles (channel_id, updated_at) VALUES ($1, $2)",
		"UC_FRESH", freshTime,
	)
	require.NoError(t, err)

	poller := NewChannelStatsPoller(client, db)
	require.NoError(t, poller.Poll(context.Background(), "UC_FRESH"))

	// fresh 프로필이므로 snippet 호출 없어야 한다
	require.Equal(t, 0, snippetCallCount)
}

func TestChannelStatsPollerHandlesScraperError(t *testing.T) {
	db := newPollerBatchTestDB(t)

	scraperErr := errors.New("network unavailable")
	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, scraperErr
	}))

	poller := NewChannelStatsPoller(client, db)
	err := poller.Poll(context.Background(), "UC_ERR")
	require.Error(t, err)

	var snapshotCount int64
	require.NoError(t, db.Model(&domain.YouTubeChannelStatsSnapshot{}).Count(&snapshotCount).Error)
	require.Zero(t, snapshotCount)
}

func TestChannelStatsPollerName(t *testing.T) {
	db := newPollerBatchTestDB(t)
	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(bothPagesTransport))
	poller := NewChannelStatsPoller(client, db)
	require.Equal(t, "channel_stats", poller.Name())
}

func TestChannelStatsPollerProxyAccessors(t *testing.T) {
	db := newPollerBatchTestDB(t)
	client := newChannelStatsTestClient(t, shortsPollerRoundTripFunc(bothPagesTransport))
	poller := NewChannelStatsPoller(client, db)
	require.False(t, poller.ProxyEnabled())
	require.False(t, poller.SetProxyEnabled(true))
}
