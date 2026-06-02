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

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ChannelStatsPoller struct {
	client          *scraper.Client
	db              pollerDB
	profileCacheTTL time.Duration
}

func NewChannelStatsPoller(scraperClient *scraper.Client, db any) *ChannelStatsPoller {
	return &ChannelStatsPoller{
		client:          scraperClient,
		db:              normalizePollerDB(db),
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
		CapturedAt:      time.Now().UTC().Truncate(time.Microsecond),
		SubscriberCount: stats.SubscriberCount,
		ViewCount:       stats.ViewCount,
		VideoCount:      stats.VideoCount,
		JoinedDate:      stats.JoinedDate,
		Description:     stats.Description,
		Country:         stats.Country,
		Handle:          stats.Handle,
	}

	if p.db == nil {
		return fmt.Errorf("failed to save channel stats snapshot: db is nil")
	}
	if _, err := p.db.Exec(ctx, `
		INSERT INTO youtube_channel_stats_snapshots
			(channel_id, captured_at, subscriber_count, view_count, video_count, joined_date, description, country, handle)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		snapshot.ChannelID,
		snapshot.CapturedAt,
		snapshot.SubscriberCount,
		snapshot.ViewCount,
		snapshot.VideoCount,
		snapshot.JoinedDate,
		snapshot.Description,
		snapshot.Country,
		snapshot.Handle,
	); err != nil {
		return fmt.Errorf("failed to save channel stats snapshot: %w", err)
	}

	slog.Debug("Channel stats snapshot saved",
		"channel_id", channelID,
		"subscriber_count", stats.SubscriberCount)

	p.updateProfileIfStale(ctx, channelID)

	return nil
}

func (p *ChannelStatsPoller) updateProfileIfStale(ctx context.Context, channelID string) {
	if p.db == nil {
		return
	}

	var profile struct {
		ChannelID string    `db:"channel_id"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err := pgxscan.Get(ctx, p.db, &profile, `
		SELECT channel_id, updated_at
		FROM youtube_channel_profiles
		WHERE channel_id = $1`,
		channelID,
	)
	needsUpdate := true
	if err == nil {
		needsUpdate = time.Since(profile.UpdatedAt) > p.profileCacheTTL
	} else if !pgxscan.NotFound(err) {
		slog.Warn("Failed to load channel profile freshness",
			"channel_id", channelID,
			"error", err)
	}

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
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := p.db.Exec(ctx, `
		INSERT INTO youtube_channel_profiles
			(channel_id, avatar, banner, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id) DO UPDATE SET
			avatar = excluded.avatar,
			banner = excluded.banner,
			updated_at = excluded.updated_at`,
		newProfile.ChannelID,
		newProfile.Avatar,
		newProfile.Banner,
		now,
	); err != nil {
		slog.Warn("Failed to update channel profile",
			"channel_id", channelID,
			"error", err)
		return
	}

	slog.Debug("Channel profile updated",
		"channel_id", channelID,
		"avatar_count", len(avatars),
		"banner_count", len(banners))
}
