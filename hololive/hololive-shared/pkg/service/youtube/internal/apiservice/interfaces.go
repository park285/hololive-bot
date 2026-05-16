package apiservice

import "context"

type Service interface {
	SetScraperProxyEnabled(enabled bool) bool
	ScraperProxyEnabled() bool
	GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error)
	GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error)
}
