package resolver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedPendingShortResolution(t *testing.T, db *batchTestDB, channelID, videoID string, detectedAt time.Time) {
	t.Helper()

	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     videoID,
		ChannelID:   channelID,
		Title:       "Short " + videoID,
		IsShort:     true,
		ViewCount:   10,
		FirstSeenAt: detectedAt,
		LastSeenAt:  detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindNewShort,
		ContentID:          "short:" + videoID,
		CanonicalContentID: "short:" + videoID,
		ChannelID:          channelID,
		DetectedAt:         detectedAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusPending,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsSourcePost{
		Kind:       domain.OutboxKindNewShort,
		PostID:     "short:" + videoID,
		ChannelID:  channelID,
		DetectedAt: detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:" + videoID,
		ContentID:      "short:" + videoID,
		ChannelID:      channelID,
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}).Error)
}

func seedResolvedShortDispatchGap(t *testing.T, db *batchTestDB, channelID, videoID string, detectedAt, publishedAt time.Time, authorizedAt *time.Time) {
	t.Helper()

	postID := "short:" + videoID
	status := domain.YouTubeCommunityShortsAlarmStateStatusDetected
	if authorizedAt != nil {
		status = domain.YouTubeCommunityShortsAlarmStateStatusEnqueued
	}

	require.NoError(t, db.Create(&domain.YouTubeVideo{
		VideoID:     videoID,
		ChannelID:   channelID,
		Title:       "title-" + videoID,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindNewShort,
		ContentID:          postID,
		CanonicalContentID: postID,
		ChannelID:          channelID,
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusPending,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsSourcePost{
		Kind:              domain.OutboxKindNewShort,
		PostID:            postID,
		ChannelID:         channelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            postID,
		ContentID:         postID,
		ChannelID:         channelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      authorizedAt,
		DeliveryStatus:    status,
	}).Error)
}

func seedPendingCommunityResolution(t *testing.T, db *batchTestDB, postID string, detectedAt time.Time) {
	t.Helper()
	const channelID = "channel-community"

	require.NoError(t, db.Create(&domain.YouTubeCommunityPost{
		PostID:       "community:" + postID,
		ChannelID:    channelID,
		AuthorName:   "Author " + postID,
		ContentText:  "Content " + postID,
		LikeCount:    10,
		CommentCount: 1,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          "community:" + postID,
		CanonicalContentID: "community:" + postID,
		ChannelID:          channelID,
		DetectedAt:         detectedAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusPending,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsSourcePost{
		Kind:       domain.OutboxKindCommunityPost,
		PostID:     "community:" + postID,
		ChannelID:  channelID,
		DetectedAt: detectedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindCommunityPost,
		PostID:         "community:" + postID,
		ContentID:      "community:" + postID,
		ChannelID:      channelID,
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}).Error)
}

func assertShortMetadataBackfilledWithoutEnqueue(
	t *testing.T,
	db *batchTestDB,
	videoID string,
	detectedAt time.Time,
	publishedAt time.Time,
	authorizedAt *time.Time,
	alarmSentAt *time.Time,
) {
	t.Helper()

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", videoID).Error)
	require.NotNil(t, video.PublishedAt)
	assert.Equal(t, publishedAt, video.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt, tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt, tracking.DetectedAt.UTC())
	if alarmSentAt == nil {
		assert.Nil(t, tracking.AlarmSentAt)
	} else {
		require.NotNil(t, tracking.AlarmSentAt)
		assertPersistedTimeEqual(t, *alarmSentAt, tracking.AlarmSentAt.UTC())
	}

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt, alarmState.ActualPublishedAt.UTC())
	if authorizedAt == nil {
		assert.Nil(t, alarmState.AuthorizedAt)
	} else {
		require.NotNil(t, alarmState.AuthorizedAt)
		assertPersistedTimeEqual(t, *authorizedAt, alarmState.AuthorizedAt.UTC())
	}
	if alarmSentAt == nil {
		assert.Nil(t, alarmState.AlarmSentAt)
	} else {
		require.NotNil(t, alarmState.AlarmSentAt)
		assertPersistedTimeEqual(t, *alarmSentAt, alarmState.AlarmSentAt.UTC())
	}
}

func assertCommunityMetadataBackfilledWithoutEnqueue(
	t *testing.T,
	db *batchTestDB,
	postID string,
	detectedAt time.Time,
	publishedAt time.Time,
	authorizedAt *time.Time,
	alarmSentAt *time.Time,
) {
	t.Helper()

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "community:"+postID).Error)
	require.NotNil(t, post.PublishedAt)
	assert.Equal(t, publishedAt.UTC(), post.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt.UTC(), tracking.DetectedAt.UTC())
	if alarmSentAt == nil {
		assert.Nil(t, tracking.AlarmSentAt)
	} else {
		require.NotNil(t, tracking.AlarmSentAt)
		assertPersistedTimeEqual(t, *alarmSentAt, tracking.AlarmSentAt.UTC())
	}

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:"+postID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt.UTC(), alarmState.ActualPublishedAt.UTC())
	if authorizedAt == nil {
		assert.Nil(t, alarmState.AuthorizedAt)
	} else {
		require.NotNil(t, alarmState.AuthorizedAt)
		assertPersistedTimeEqual(t, *authorizedAt, alarmState.AuthorizedAt.UTC())
	}
	if alarmSentAt == nil {
		assert.Nil(t, alarmState.AlarmSentAt)
	} else {
		require.NotNil(t, alarmState.AlarmSentAt)
		assertPersistedTimeEqual(t, *alarmSentAt, alarmState.AlarmSentAt.UTC())
	}
}

func assertPersistedTimeEqual(t *testing.T, expected, actual time.Time) {
	t.Helper()

	assert.Equal(t, expected.UTC().Truncate(time.Microsecond), actual.UTC().Truncate(time.Microsecond))
}

func newShortPublishedAtResolverTestClient(t *testing.T, publishedAt time.Time, resolveCalls *int) *scraper.Client {
	t.Helper()

	watchHTML := `<html><head><meta itemprop="uploadDate" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return newShortPublishedAtResolverHTTPClient(t, watchHTML, resolveCalls)
}

func newCommunityPublishedAtResolverTestClient(t *testing.T, publishedAt time.Time, resolveCalls *int) *scraper.Client {
	t.Helper()

	postHTML := `<html><head><meta itemprop="datePublished" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasPrefix(req.URL.Path, "/post/") {
					if resolveCalls != nil {
						(*resolveCalls)++
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(postHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func newShortPublishedAtResolverHTTPClient(t *testing.T, watchHTML string, resolveCalls ...*int) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					if len(resolveCalls) > 0 && resolveCalls[0] != nil {
						(*resolveCalls[0])++
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func newShortPublishedAtResolverDelayedClient(t *testing.T, publishedAt time.Time, delay time.Duration, resolveCalls *int) *scraper.Client {
	t.Helper()

	watchHTML := `<html><head><meta itemprop="uploadDate" content="` + publishedAt.Format(time.RFC3339) + `"></head></html>`
	return newShortPublishedAtResolverDelayedHTTPClient(t, watchHTML, delay, resolveCalls)
}

func newShortPublishedAtResolverDelayedHTTPClient(t *testing.T, watchHTML string, delay time.Duration, resolveCalls *int) *scraper.Client {
	t.Helper()

	return newShortPublishedAtResolverDelayedHTTPClientWithCallback(t, watchHTML, delay, func() {
		if resolveCalls != nil {
			(*resolveCalls)++
		}
	})
}

func newShortPublishedAtResolverDelayedHTTPClientWithCallback(t *testing.T, watchHTML string, delay time.Duration, onResolve func()) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-timer.C:
					}
					if onResolve != nil {
						onResolve()
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(watchHTML)), Header: make(http.Header), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func newShortPublishedAtResolverErrorClient(t *testing.T) *scraper.Client {
	t.Helper()

	return scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithUAProvider(ua.NewStaticProvider("test-agent")),
		scraper.WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: shortsPollerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/watch" {
					return nil, assert.AnError
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: req}, nil
			}),
		}),
	)
}

func assertResolvedShortState(t *testing.T, db *batchTestDB, channelID, videoID string, detectedAt, publishedAt time.Time) {
	t.Helper()

	var video domain.YouTubeVideo
	require.NoError(t, db.First(&video, "video_id = ?", videoID).Error)
	require.NotNil(t, video.PublishedAt)
	assert.Equal(t, publishedAt, video.PublishedAt.UTC())

	var tracking domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&tracking, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, tracking.ActualPublishedAt)
	assert.Equal(t, publishedAt, tracking.ActualPublishedAt.UTC())
	assert.Equal(t, detectedAt, tracking.DetectedAt.UTC())

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	assert.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())

	var alarmState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&alarmState, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:"+videoID).Error)
	require.NotNil(t, alarmState.ActualPublishedAt)
	assert.Equal(t, publishedAt, alarmState.ActualPublishedAt.UTC())
	assert.Nil(t, alarmState.AuthorizedAt)
	assert.Equal(t, channelID, alarmState.ChannelID)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, alarmState.DeliveryStatus)
}
