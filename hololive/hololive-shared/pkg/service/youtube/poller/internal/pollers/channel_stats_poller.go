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
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ChannelStatsPoller struct {
	client          *scraper.Client
	db              *gorm.DB
	profileCacheTTL time.Duration
}

func NewChannelStatsPoller(scraperClient *scraper.Client, db *gorm.DB) *ChannelStatsPoller {
	return &ChannelStatsPoller{
		client:          scraperClient,
		db:              db,
		profileCacheTTL: 24 * time.Hour,
	}
}

func (p *ChannelStatsPoller) Name() string {
	return "channel_stats"
}

func (p *ChannelStatsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *ChannelStatsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *ChannelStatsPoller) Poll(ctx context.Context, channelID string) error {
	stats, err := p.client.GetChannelStats(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel stats: %w", err)
	}

	snapshot := &domain.YouTubeChannelStatsSnapshot{
		ChannelID:       channelID,
		CapturedAt:      time.Now(),
		SubscriberCount: stats.SubscriberCount,
		ViewCount:       stats.ViewCount,
		VideoCount:      stats.VideoCount,
		JoinedDate:      stats.JoinedDate,
		Description:     stats.Description,
		Country:         stats.Country,
		Handle:          stats.Handle,
	}

	if err := p.db.WithContext(ctx).Create(snapshot).Error; err != nil {
		return fmt.Errorf("failed to save channel stats snapshot: %w", err)
	}

	slog.Debug("Channel stats snapshot saved",
		"channel_id", channelID,
		"subscriber_count", stats.SubscriberCount)

	p.updateProfileIfStale(ctx, channelID)

	return nil
}

func (p *ChannelStatsPoller) updateProfileIfStale(ctx context.Context, channelID string) {
	var profile domain.YouTubeChannelProfile
	err := p.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&profile).Error

	needsUpdate := err != nil || time.Since(profile.UpdatedAt) > p.profileCacheTTL

	if !needsUpdate {
		return
	}

	snippet, err := p.client.GetChannelSnippet(ctx, channelID)
	if err != nil {
		slog.Warn("Failed to get channel snippet for profile update",
			"channel_id", channelID,
			"error", err)
		return
	}

	avatars := polling.ConvertThumbnails(snippet.Avatar)
	banners := polling.ConvertThumbnails(snippet.Banner)

	newProfile := &domain.YouTubeChannelProfile{
		ChannelID: channelID,
		Avatar:    avatars,
		Banner:    banners,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"avatar", "banner", "updated_at"}),
	}).Create(newProfile)

	slog.Debug("Channel profile updated",
		"channel_id", channelID,
		"avatar_count", len(avatars),
		"banner_count", len(banners))
}
