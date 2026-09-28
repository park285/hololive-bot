package settings

import "time"

type HolodexConfig struct {
	BaseURL              string
	APIKey               string
	Timeout              time.Duration
	PerAttemptTimeout    time.Duration
	MaxRetryAttempts     int
	Transport            HolodexTransportConfig
	Concurrency          HolodexConcurrencyConfig
	DistributedRateLimit DistributedRateLimitConfig
}

type YouTubeConfig struct {
	MaxPageBodyBytes     int64
	ScraperHTTPTimeout   time.Duration
	ScraperDialTimeout   time.Duration
	ScraperHeaderTimeout time.Duration
	CommunityMissingTTL  time.Duration
	RequestInterval      time.Duration
	DistributedRateLimit DistributedRateLimitConfig
}

type IngestionConfig struct {
	PhotoSyncEnabled bool
}
