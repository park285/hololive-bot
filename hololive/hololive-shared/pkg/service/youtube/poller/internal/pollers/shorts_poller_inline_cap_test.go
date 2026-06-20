package pollers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

func shortsHTMLForVideoIDs(videoIDs ...string) string {
	items := make([]string, 0, len(videoIDs))
	for _, id := range videoIDs {
		items = append(items, `{"richItemRenderer":{"content":{"shortsLockupViewModel":{"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"`+id+`"}}},"overlayMetadata":{"primaryText":{"content":"`+id+`"},"secondaryText":{"content":"1.2K views"}},"thumbnail":{"sources":[{"url":"https://img.test/`+id+`.jpg","width":120,"height":200}]}}}}}`)
	}
	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[` + strings.Join(items, ",") + `]}}}}]}}}`
	return "<script>var ytInitialData = " + shortsJSON + ";</script>"
}

func TestHB05InlineShortsResolveCappedPerPoll_617c2dd4(t *testing.T) {
	db := newPollerBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
		&domain.YouTubeContentAlarmTracking{},
		&domain.YouTubeCommunityShortsSourcePost{},
		&domain.YouTubeCommunityShortsAlarmState{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     "UC_TEST",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "old-short",
	}).Error)

	videoIDs := []string{"s-1", "s-2", "s-3", "s-4", "s-5", "s-6"}
	shortsHTML := shortsHTMLForVideoIDs(videoIDs...)
	emptyRSS := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
</feed>`
	watchHTML := `<html><head><meta itemprop="uploadDate" content="2026-04-10T10:11:12+09:00"></head></html>`
	resolveCalls := 0

	client := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(req.URL.Path, "/shorts"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(shortsHTML)), Header: make(http.Header), Request: req}, nil
				case strings.HasSuffix(req.URL.Path, "/feeds/videos.xml"):
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(emptyRSS)), Header: make(http.Header), Request: req}, nil
				case req.URL.Path == "/watch":
					resolveCalls++
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
				}
			}),
		}),
	)

	poller := NewShortsPoller(client, db, 10, func(NotificationRouteRequest) bool { return true }, true)
	require.NoError(t, poller.Poll(context.Background(), "UC_TEST"))

	assert.LessOrEqual(t, resolveCalls, MaxInlinePublishedAtResolvesPerPoll,
		"single poll must cap inline scraper resolves at MaxInlinePublishedAtResolvesPerPoll, got %d for %d missing-published_at shorts", resolveCalls, len(videoIDs))

	var stored int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Where("channel_id = ?", "UC_TEST").Count(&stored).Error)
	assert.Equal(t, int64(len(videoIDs)), stored,
		"all detected shorts must still be persisted even when inline resolve is capped")
}
