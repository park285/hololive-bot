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

package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type LivePoller struct {
	client *scraper.Client
	db     *gorm.DB
}

func NewLivePoller(scraperClient *scraper.Client, db *gorm.DB) *LivePoller {
	return &LivePoller{
		client: scraperClient,
		db:     db,
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
	events, err := p.client.GetUpcomingEvents(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get upcoming events: %w", err)
	}

	now := time.Now()

	for _, event := range events {
		var status domain.LiveStatus
		switch event.Status {
		case "LIVE":
			status = domain.LiveStatusLive
		case "UPCOMING":
			status = domain.LiveStatusUpcoming
		default:
			continue // LIVE나 UPCOMING이 아니면 스킵
		}

		// 스케줄 시작 시간
		var scheduledStart *time.Time
		if event.StartTime != nil {
			t := time.Unix(*event.StartTime, 0)
			scheduledStart = &t
		}

		// 라이브 세션 upsert
		session := &domain.YouTubeLiveSession{
			VideoID:            event.VideoID,
			ChannelID:          channelID,
			Status:             status,
			Title:              event.Title,
			ScheduledStartTime: scheduledStart,
		}

		// LIVE 상태면 시작 시간 기록
		if status == domain.LiveStatusLive {
			session.StartedAt = &now
		}

		p.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "video_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "title", "scheduled_start_time", "started_at", "last_seen_at"}),
		}).Create(session)

		// LIVE 상태면 시청자 샘플 저장
		if status == domain.LiveStatusLive {
			viewerCount := parseViewerCount(event.ViewCountText)
			if viewerCount > 0 {
				sample := &domain.YouTubeLiveViewerSample{
					VideoID:           event.VideoID,
					CapturedAt:        now,
					ChannelID:         channelID,
					ConcurrentViewers: viewerCount,
				}

				p.db.WithContext(ctx).Clauses(clause.OnConflict{
					DoNothing: true,
				}).Create(sample)

				slog.Debug("Live viewer sample saved",
					"video_id", event.VideoID,
					"viewers", viewerCount)
			}
		}
	}

	// 더 이상 보이지 않는 LIVE 세션을 ENDED로 전환
	p.markEndedSessions(ctx, channelID, events)

	return nil
}

// markEndedSessions: 종료된 세션 마킹
func (p *LivePoller) markEndedSessions(ctx context.Context, channelID string, currentEvents []*scraper.UpcomingEvent) {
	// 현재 활성 비디오 ID 수집
	activeIDs := make(map[string]bool)
	for _, e := range currentEvents {
		if e.Status == "LIVE" || e.Status == "UPCOMING" {
			activeIDs[e.VideoID] = true
		}
	}

	// DB에서 해당 채널의 LIVE 세션 조회
	var liveSessions []domain.YouTubeLiveSession
	p.db.WithContext(ctx).Where(
		"channel_id = ? AND status = ?",
		channelID, domain.LiveStatusLive,
	).Find(&liveSessions)

	now := time.Now()
	for _, session := range liveSessions {
		if !activeIDs[session.VideoID] {
			// 더 이상 LIVE가 아님 - ENDED로 전환
			p.db.WithContext(ctx).Model(&session).Updates(map[string]any{
				"status":   domain.LiveStatusEnded,
				"ended_at": now,
			})

			// 스트림 통계 집계
			p.finalizeStreamStats(ctx, session.VideoID, channelID)
		}
	}
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
