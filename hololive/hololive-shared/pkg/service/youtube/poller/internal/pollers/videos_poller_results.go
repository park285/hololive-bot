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

package pollers

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"

	"github.com/kapu/hololive-shared/internal/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type videoPollResult struct {
	dbVideo      *domain.YouTubeVideo
	notification *domain.YouTubeNotificationOutbox
	deferred     bool
}

func (p *VideosPoller) buildVideoPollResults(
	ctx context.Context,
	channelID string,
	newVideos []*scraper.Video,
	knownVideoIDs map[string]struct{},
	isInitialized bool,
	resolutionByID map[string]videoResolution,
	now time.Time,
) ([]*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, map[string]struct{}) {
	dbVideos := make([]*domain.YouTubeVideo, 0, len(newVideos))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newVideos))
	deferredVideoIDs := make(map[string]struct{})

	for _, video := range newVideos {
		result := p.buildVideoPollResult(ctx, channelID, video, knownVideoIDs, isInitialized, resolutionByID, now)
		if result.dbVideo != nil {
			dbVideos = append(dbVideos, result.dbVideo)
		}

		if result.notification != nil {
			notifications = append(notifications, result.notification)
		}

		if result.deferred {
			deferredVideoIDs[video.VideoID] = struct{}{}
		}
	}

	return dbVideos, notifications, deferredVideoIDs
}

func (p *VideosPoller) buildVideoPollResult(
	ctx context.Context,
	channelID string,
	video *scraper.Video,
	knownVideoIDs map[string]struct{},
	isInitialized bool,
	resolutionByID map[string]videoResolution,
	now time.Time,
) videoPollResult {
	isLiveReplay := polling.IsLiveReplayVideo(video.PublishedText)
	dbVideo := buildYouTubeVideo(channelID, video, isLiveReplay)
	if !isInitialized || isLiveReplay {
		return videoPollResult{dbVideo: dbVideo}
	}

	if _, known := knownVideoIDs[video.VideoID]; known {
		p.deferrals.clear(channelID, video.VideoID)

		return videoPollResult{dbVideo: dbVideo}
	}

	pageEvidence := videoPublishedTextEvidence(video.PublishedText, now)
	resolution := resolutionByID[video.VideoID]
	if resolution.replay == scraper.ReplayStatusReplay {
		p.deferrals.clear(channelID, video.VideoID)
		dbVideo.IsLiveReplay = true
		dbVideo.PublishedAt = resolvedVideoPublishedAt(pageEvidence, resolution)

		return videoPollResult{dbVideo: dbVideo}
	}

	return p.buildVideoFreshnessResult(ctx, channelID, video, dbVideo, pageEvidence, resolution, now)
}

func (p *VideosPoller) buildVideoFreshnessResult(
	ctx context.Context,
	channelID string,
	video *scraper.Video,
	dbVideo *domain.YouTubeVideo,
	pageEvidence videoPublicationEvidence,
	resolution videoResolution,
	now time.Time,
) videoPollResult {
	evidence := resolvedVideoPublicationEvidence(pageEvidence, resolution, now)
	if evidence.freshness == videoFreshnessFresh && videoNeedsWatchMetadata(video, pageEvidence) && resolution.replay != scraper.ReplayStatusNotReplay {
		evidence = videoPublicationEvidence{freshness: videoFreshnessUnresolved}
	}

	if evidence.freshness == videoFreshnessUnresolved {
		return p.buildDeferredVideoResult(ctx, channelID, video.VideoID, dbVideo)
	}

	p.deferrals.clear(channelID, video.VideoID)
	dbVideo.PublishedAt = evidence.publishedAt
	result := videoPollResult{dbVideo: dbVideo}

	if evidence.freshness == videoFreshnessFresh {
		result.notification = buildVideoNotification(channelID, dbVideo)
	}

	return result
}

func (p *VideosPoller) buildDeferredVideoResult(
	ctx context.Context,
	channelID string,
	videoID string,
	dbVideo *domain.YouTubeVideo,
) videoPollResult {
	attempts := p.deferrals.recordFailure(channelID, videoID)
	if attempts < publicationFreshnessMaxAttempts {
		slog.WarnContext(ctx, "Video freshness unresolved; deferring candidate without notification",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
			"attempts", attempts,
		)

		return videoPollResult{deferred: true}
	}

	p.deferrals.clear(channelID, videoID)
	slog.WarnContext(ctx, "Video freshness unresolved after max attempts; absorbing silently without notification",
		logschema.FieldChannelID, channelID,
		"video_id", videoID,
		"attempts", attempts,
	)

	return videoPollResult{dbVideo: dbVideo}
}

func resolvedVideoPublishedAt(pageEvidence videoPublicationEvidence, resolution videoResolution) *time.Time {
	if resolution.publishedAt != nil {
		return resolution.publishedAt
	}

	return pageEvidence.publishedAt
}
