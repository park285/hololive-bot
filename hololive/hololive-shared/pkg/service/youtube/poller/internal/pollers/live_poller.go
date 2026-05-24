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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type LivePoller struct {
	client             *scraper.Client
	liveStatusProvider LiveStatusProvider
	db                 *gorm.DB
	baselineMu         sync.Mutex
	baselinedChannels  map[string]struct{}
}

type LiveStatusProvider interface {
	GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error)
}

func NewLivePoller(scraperClient *scraper.Client, db *gorm.DB) *LivePoller {
	return NewLivePollerWithStatusProvider(nil, scraperClient, db)
}

func NewLivePollerWithStatusProvider(provider LiveStatusProvider, scraperClient *scraper.Client, db *gorm.DB) *LivePoller {
	return &LivePoller{
		client:             scraperClient,
		liveStatusProvider: provider,
		db:                 db,
		baselinedChannels:  make(map[string]struct{}),
	}
}

func (p *LivePoller) Name() string {
	return "live"
}

func (p *LivePoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *LivePoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *LivePoller) Poll(ctx context.Context, channelID string) error {
	streams, err := p.fetchLiveStreams(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get live streams: %w", err)
	}

	now := time.Now()
	baselinePoll := p.isBaselinePoll(channelID)

	for _, stream := range streams {
		if err := p.pollStream(ctx, channelID, stream, now, baselinePoll); err != nil {
			return err
		}
	}

	p.markEndedSessions(ctx, channelID, streams)
	p.markBaselineComplete(channelID)

	return nil
}

func (p *LivePoller) isBaselinePoll(channelID string) bool {
	p.baselineMu.Lock()
	defer p.baselineMu.Unlock()

	_, exists := p.baselinedChannels[channelID]
	return !exists
}

func (p *LivePoller) markBaselineComplete(channelID string) {
	p.baselineMu.Lock()
	defer p.baselineMu.Unlock()

	p.baselinedChannels[channelID] = struct{}{}
}

func (p *LivePoller) fetchLiveStreams(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	if p.liveStatusProvider != nil {
		return p.liveStatusProvider.GetChannelsLiveStatus(ctx, []string{channelID})
	}
	if p.client == nil {
		return nil, errors.New("live poller has no status provider or scraper client")
	}

	events, err := p.client.GetUpcomingEvents(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return streamsFromUpcomingEvents(channelID, events), nil
}

func (p *LivePoller) pollStream(ctx context.Context, channelID string, stream *domain.Stream, now time.Time, baselinePoll bool) error {
	status, ok := liveStatusFromStream(stream)
	if !ok {
		return nil
	}

	if err := p.saveLiveSession(ctx, channelID, stream, status, now, baselinePoll); err != nil {
		return fmt.Errorf("poll live stream %s: %w", stream.ID, err)
	}

	if status == domain.LiveStatusLive {
		p.saveLiveViewerSample(ctx, channelID, stream, now)
	}

	return nil
}

func liveStatusFromStream(stream *domain.Stream) (domain.LiveStatus, bool) {
	if stream == nil {
		return "", false
	}
	switch stream.Status {
	case domain.StreamStatusLive:
		return domain.LiveStatusLive, true
	case domain.StreamStatusUpcoming:
		return domain.LiveStatusUpcoming, true
	default:
		return "", false
	}
}

func (p *LivePoller) saveLiveSession(ctx context.Context, channelID string, stream *domain.Stream, status domain.LiveStatus, now time.Time, baselinePoll bool) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, _, err := loadExistingLiveSession(ctx, tx, stream.ID)
		if err != nil {
			return err
		}

		session := buildLiveSession(channelID, stream, status, now, existing)
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "video_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"status":               gorm.Expr("excluded.status"),
				"title":                gorm.Expr("excluded.title"),
				"scheduled_start_time": gorm.Expr("excluded.scheduled_start_time"),
				"started_at":           gorm.Expr("excluded.started_at"),
				"live_first_seen_at":   gorm.Expr("COALESCE(youtube_live_sessions.live_first_seen_at, excluded.live_first_seen_at)"),
				"last_seen_at":         gorm.Expr("excluded.last_seen_at"),
			}),
		}).Create(session).Error; err != nil {
			return fmt.Errorf("save live session: %w", err)
		}

		return nil
	})
}

func loadExistingLiveSession(ctx context.Context, tx *gorm.DB, videoID string) (domain.YouTubeLiveSession, bool, error) {
	var existing domain.YouTubeLiveSession
	err := tx.WithContext(ctx).Where("video_id = ?", videoID).First(&existing).Error
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.YouTubeLiveSession{}, false, nil
	}
	return domain.YouTubeLiveSession{}, false, fmt.Errorf("load existing live session: %w", err)
}

func buildLiveSession(channelID string, stream *domain.Stream, status domain.LiveStatus, now time.Time, existing domain.YouTubeLiveSession) *domain.YouTubeLiveSession {
	session := &domain.YouTubeLiveSession{
		VideoID:            stream.ID,
		ChannelID:          firstNonEmpty(stream.ChannelID, channelID),
		Status:             status,
		Title:              stream.Title,
		ScheduledStartTime: stream.StartScheduled,
		LiveFirstSeenAt:    liveFirstSeenAt(status, now, existing),
	}

	if status == domain.LiveStatusLive {
		session.StartedAt = liveStartedAt(stream, now, existing)
	}

	return session
}

func liveFirstSeenAt(status domain.LiveStatus, now time.Time, existing domain.YouTubeLiveSession) *time.Time {
	if existing.LiveFirstSeenAt != nil && !existing.LiveFirstSeenAt.IsZero() {
		value := existing.LiveFirstSeenAt.UTC()
		return &value
	}
	if status != domain.LiveStatusLive {
		return nil
	}
	value := now.UTC()
	return &value
}

func firstNonEmpty(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func liveStartedAt(stream *domain.Stream, now time.Time, existing domain.YouTubeLiveSession) *time.Time {
	if existing.StartedAt != nil && !existing.StartedAt.IsZero() {
		startedAt := existing.StartedAt.UTC()
		return &startedAt
	}
	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		startedAt := stream.StartActual.UTC()
		return &startedAt
	}
	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		startedAt := stream.StartScheduled.UTC()
		return &startedAt
	}
	startedAt := now.UTC()
	return &startedAt
}

func (p *LivePoller) saveLiveViewerSample(ctx context.Context, channelID string, stream *domain.Stream, now time.Time) {
	if stream == nil || stream.ViewerCount == nil || *stream.ViewerCount <= 0 {
		return
	}

	sample := &domain.YouTubeLiveViewerSample{
		VideoID:           stream.ID,
		CapturedAt:        now,
		ChannelID:         firstNonEmpty(stream.ChannelID, channelID),
		ConcurrentViewers: *stream.ViewerCount,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(sample)

	slog.Debug("Live viewer sample saved",
		"video_id", stream.ID,
		"viewers", *stream.ViewerCount)
}

func (p *LivePoller) markEndedSessions(ctx context.Context, channelID string, currentStreams []*domain.Stream) {
	activeIDs := activeLiveStreamIDs(currentStreams)

	var liveSessions []domain.YouTubeLiveSession
	p.db.WithContext(ctx).Where(
		"channel_id = ? AND status = ?",
		channelID, domain.LiveStatusLive,
	).Find(&liveSessions)

	now := time.Now()
	for _, session := range liveSessions {
		if !activeIDs[session.VideoID] {
			p.db.WithContext(ctx).Model(&session).Updates(map[string]any{
				"status":   domain.LiveStatusEnded,
				"ended_at": now,
			})

			p.finalizeStreamStats(ctx, session.VideoID, channelID)
		}
	}
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

// finalizeStreamStats: 스트림 통계 집계
func (p *LivePoller) finalizeStreamStats(ctx context.Context, videoID, channelID string) {
	// 샘플 데이터 집계
	var result struct {
		MaxViewers int
		AvgViewers int
		Count      int
	}

	p.db.WithContext(ctx).Model(&domain.YouTubeLiveViewerSample{}).
		Select("MAX(concurrent_viewers) as max_viewers, AVG(concurrent_viewers) as avg_viewers, COUNT(*) as count").
		Where("video_id = ?", videoID).
		Scan(&result)

	if result.Count == 0 {
		return
	}

	// 스트림 통계 저장
	var session domain.YouTubeLiveSession
	p.db.WithContext(ctx).Where("video_id = ?", videoID).First(&session)

	stats := &domain.YouTubeStreamStats{
		VideoID:              videoID,
		ChannelID:            channelID,
		StartedAt:            session.StartedAt,
		EndedAt:              session.EndedAt,
		MaxConcurrentViewers: result.MaxViewers,
		AvgConcurrentViewers: result.AvgViewers,
		SampleCount:          result.Count,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "video_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ended_at", "max_concurrent_viewers", "avg_concurrent_viewers", "sample_count", "updated_at"}),
	}).Create(stats)

	slog.Info("Stream stats finalized",
		"video_id", videoID,
		"max_viewers", result.MaxViewers,
		"avg_viewers", result.AvgViewers)
}
