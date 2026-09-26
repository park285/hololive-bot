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
	LiveStatusFallback   HolodexLiveStatusFallbackConfig
}

type HolodexLiveStatusFallbackConfig struct {
	MaxPerCycle     int
	WallClockBudget time.Duration
	DeadlineMargin  time.Duration
}

type YouTubeConfig struct {
	CacheExpiration      time.Duration
	MaxPageBodyBytes     int64
	ScraperHTTPTimeout   time.Duration
	ScraperDialTimeout   time.Duration
	ScraperHeaderTimeout time.Duration
	ScraperPhaseTimeout  time.Duration
	CacheSaveTimeout     time.Duration
	CommunityMissingTTL  time.Duration
	VideoRSSBackoffTTL   time.Duration
	RequestInterval      time.Duration
	DistributedRateLimit DistributedRateLimitConfig
}

type IngestionConfig struct {
	PhotoSyncEnabled                bool
	CommunityShortsBigBangCutoverAt time.Time
}
