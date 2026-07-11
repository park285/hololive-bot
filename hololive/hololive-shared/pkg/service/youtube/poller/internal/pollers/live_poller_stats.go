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

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (p *LivePoller) saveLiveViewerSample(ctx context.Context, channelID string, stream *domain.Stream, now time.Time) {
	if stream == nil || stream.ViewerCount == nil || *stream.ViewerCount <= 0 {
		return
	}

	sample := &domain.YouTubeLiveViewerSample{
		VideoID:           stream.ID,
		CapturedAt:        now.UTC().Truncate(time.Microsecond),
		ChannelID:         firstNonEmpty(stream.ChannelID, channelID),
		ConcurrentViewers: *stream.ViewerCount,
	}

	if p.db == nil {
		slog.Warn("Live viewer sample skipped because db is nil", "video_id", stream.ID)
		return
	}
	if _, err := p.db.Exec(ctx, mustSQL("live_poller_stats_0049_01.sql"),
		sample.VideoID,
		sample.CapturedAt,
		sample.ChannelID,
		sample.ConcurrentViewers,
	); err != nil {
		slog.Warn("Failed to save live viewer sample", "video_id", stream.ID, "error", err)
		return
	}

	slog.Debug("Live viewer sample saved",
		"video_id", stream.ID,
		"viewers", *stream.ViewerCount)
}

func (p *LivePoller) markEndedSessions(ctx context.Context, channelID string, currentStreams []*domain.Stream, pollStartedAt time.Time) {
	activeIDs := activeLiveStreamIDs(currentStreams)

	var liveSessions []domain.YouTubeLiveSession
	if p.db == nil {
		return
	}
	if err := pgxscan.Select(ctx, p.db, &liveSessions, liveSessionSelectSQL+`
		WHERE channel_id = $1 AND status = $2`,
		channelID,
		domain.LiveStatusLive,
	); err != nil {
		slog.Warn("Failed to load live sessions for end marking", "channel_id", channelID, "error", err)
		return
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	fence := pollStartedAt.UTC()
	for i := range liveSessions {
		session := &liveSessions[i]
		p.endStaleSession(ctx, channelID, session.VideoID, activeIDs, now, fence)
	}
}

func (p *LivePoller) endStaleSession(ctx context.Context, channelID, videoID string, activeIDs map[string]bool, now, pollStartedAt time.Time) {
	if activeIDs[videoID] {
		return
	}
	if !p.markSessionEnded(ctx, videoID, now, pollStartedAt) {
		return
	}
	p.finalizeStreamStats(ctx, videoID, channelID)
}

func (p *LivePoller) markSessionEnded(ctx context.Context, videoID string, now, pollStartedAt time.Time) bool {
	tag, err := p.db.Exec(ctx, mustSQL("live_poller_stats_0102_02.sql"),
		domain.LiveStatusEnded,
		now,
		videoID,
		pollStartedAt,
	)
	if err != nil {
		slog.Warn("Failed to mark live session ended", "video_id", videoID, "error", err)
		return false
	}
	return tag.RowsAffected() > 0
}

func activeLiveStreamIDs(currentStreams []*domain.Stream) map[string]bool {
	activeIDs := make(map[string]bool)
	for _, stream := range currentStreams {
		if isActiveLiveStream(stream) {
			activeIDs[stream.ID] = true
		}
	}
	return activeIDs
}

func isActiveLiveStream(stream *domain.Stream) bool {
	return stream != nil && (stream.Status == domain.StreamStatusLive || stream.Status == domain.StreamStatusUpcoming)
}

type liveViewerStatsResult struct {
	MaxViewers int `db:"max_viewers"`
	AvgViewers int `db:"avg_viewers"`
	Count      int `db:"count"`
}

// finalizeStreamStats: 스트림 통계 집계
func (p *LivePoller) finalizeStreamStats(ctx context.Context, videoID, channelID string) {
	if p.db == nil {
		return
	}

	result, ok := p.aggregateLiveViewerStats(ctx, videoID)
	if !ok || result.Count == 0 {
		return
	}

	session, found, err := loadExistingLiveSession(ctx, p.db, videoID)
	if err != nil {
		slog.Warn("Failed to load live session for stream stats", "video_id", videoID, "error", err)
		return
	}
	if !found {
		return
	}

	p.saveStreamStats(ctx, videoID, channelID, &session, result)
}

func (p *LivePoller) aggregateLiveViewerStats(ctx context.Context, videoID string) (liveViewerStatsResult, bool) {
	var result liveViewerStatsResult
	if err := pgxscan.Get(ctx, p.db, &result, mustSQL("live_poller_stats_0161_03.sql"),
		videoID,
	); err != nil {
		slog.Warn("Failed to aggregate live stream stats", "video_id", videoID, "error", err)
		return liveViewerStatsResult{}, false
	}
	return result, true
}

func (p *LivePoller) saveStreamStats(ctx context.Context, videoID, channelID string, session *domain.YouTubeLiveSession, result liveViewerStatsResult) {
	stats := &domain.YouTubeStreamStats{
		VideoID:              videoID,
		ChannelID:            channelID,
		StartedAt:            session.StartedAt,
		EndedAt:              session.EndedAt,
		MaxConcurrentViewers: result.MaxViewers,
		AvgConcurrentViewers: result.AvgViewers,
		SampleCount:          result.Count,
	}
	stats.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)

	if _, err := p.db.Exec(ctx, mustSQL("live_poller_stats_0188_04.sql"),
		stats.VideoID,
		stats.ChannelID,
		stats.StartedAt,
		stats.EndedAt,
		stats.MaxConcurrentViewers,
		stats.AvgConcurrentViewers,
		stats.SampleCount,
		stats.UpdatedAt,
	); err != nil {
		slog.Warn("Failed to save live stream stats", "video_id", videoID, "error", err)
		return
	}

	slog.Info("Stream stats finalized",
		"video_id", videoID,
		"max_viewers", result.MaxViewers,
		"avg_viewers", result.AvgViewers)
}
