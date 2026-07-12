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

	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type videoResolution struct {
	publishedAt *time.Time
	replay      scraper.ReplayStatus
}

func (p *VideosPoller) resolveVideoMetadata(
	ctx context.Context,
	channelID string,
	videos []*scraper.Video,
	knownVideoIDs map[string]struct{},
	isInitialized bool,
	now time.Time,
) map[string]videoResolution {
	if !isInitialized {
		return nil
	}

	resolutionByID := p.resolveVideoRSSPublicationTimes(ctx, channelID, videos, knownVideoIDs, now)
	for _, video := range videos {
		p.resolveVideoWatchMetadata(ctx, channelID, video, knownVideoIDs, resolutionByID, now)
	}

	return resolutionByID
}

func (p *VideosPoller) resolveVideoRSSPublicationTimes(
	ctx context.Context,
	channelID string,
	videos []*scraper.Video,
	knownVideoIDs map[string]struct{},
	now time.Time,
) map[string]videoResolution {
	resolutionByID := make(map[string]videoResolution)
	if !videosNeedRSSPublicationResolve(videos, knownVideoIDs, now) {
		return resolutionByID
	}

	publishedAtByID, err := p.client.GetRecentVideoPublishedTimes(ctx, channelID, p.maxResults)
	if err != nil {
		slog.WarnContext(ctx, "Video RSS published_at resolve failed",
			logschema.FieldChannelID, channelID,
			"error", err.Error(),
		)

		return resolutionByID
	}

	for videoID, publishedAt := range publishedAtByID {
		normalized := publishedAt.UTC()
		resolutionByID[videoID] = videoResolution{publishedAt: &normalized}
	}

	return resolutionByID
}

func (p *VideosPoller) resolveVideoWatchMetadata(
	ctx context.Context,
	channelID string,
	video *scraper.Video,
	knownVideoIDs map[string]struct{},
	resolutionByID map[string]videoResolution,
	now time.Time,
) {
	if video == nil || polling.IsLiveReplayVideo(video.PublishedText) {
		return
	}

	if _, known := knownVideoIDs[video.VideoID]; known {
		return
	}

	pageEvidence := videoPublishedTextEvidence(video.PublishedText, now)
	resolution := resolutionByID[video.VideoID]
	effectiveEvidence := resolvedVideoPublicationEvidence(pageEvidence, resolution, now)
	if effectiveEvidence.freshness == videoFreshnessHistorical || !videoNeedsWatchMetadata(video, pageEvidence) {
		return
	}

	metadata, err := p.client.GetVideoMetadata(ctx, channelID, video.VideoID)
	if metadata.PublishedAt != nil {
		normalized := metadata.PublishedAt.UTC()
		resolution.publishedAt = &normalized
	}

	resolution.replay = metadata.Replay
	resolutionByID[video.VideoID] = resolution

	if err != nil {
		slog.WarnContext(ctx, "Video watch metadata resolve failed",
			logschema.FieldChannelID, channelID,
			"video_id", video.VideoID,
			"error", err.Error(),
		)
	}
}

func videosNeedRSSPublicationResolve(videos []*scraper.Video, knownVideoIDs map[string]struct{}, now time.Time) bool {
	for _, video := range videos {
		if videoNeedsRSSPublicationResolve(video, knownVideoIDs, now) {
			return true
		}
	}

	return false
}

func videoNeedsRSSPublicationResolve(video *scraper.Video, knownVideoIDs map[string]struct{}, now time.Time) bool {
	if video == nil || polling.IsLiveReplayVideo(video.PublishedText) {
		return false
	}

	if _, known := knownVideoIDs[video.VideoID]; known {
		return false
	}

	return video.Source != scraper.VideoSourceRSS &&
		videoPublishedTextEvidence(video.PublishedText, now).freshness == videoFreshnessUnresolved
}

func videoNeedsWatchMetadata(video *scraper.Video, pageEvidence videoPublicationEvidence) bool {
	return video != nil && (video.Source == scraper.VideoSourceRSS || pageEvidence.freshness == videoFreshnessUnresolved)
}

func resolvedVideoPublicationEvidence(
	pageEvidence videoPublicationEvidence,
	resolution videoResolution,
	now time.Time,
) videoPublicationEvidence {
	if pageEvidence.freshness != videoFreshnessUnresolved || resolution.publishedAt == nil {
		return pageEvidence
	}

	return videoPublicationEvidence{
		freshness:   classifyVideoPublishedAt(*resolution.publishedAt, now),
		publishedAt: resolution.publishedAt,
	}
}
