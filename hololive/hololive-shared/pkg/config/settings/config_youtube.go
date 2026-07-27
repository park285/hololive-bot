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
	CacheExpiration              time.Duration
	MaxPageBodyBytes             int64
	ScraperHTTPTimeout           time.Duration
	ScraperDialTimeout           time.Duration
	ScraperHeaderTimeout         time.Duration
	ScraperPhaseTimeout          time.Duration
	CacheSaveTimeout             time.Duration
	CommunityMissingTTL          time.Duration
	VideoRSSBackoffTTL           time.Duration
	ProducerRequestInterval      time.Duration
	ProducerDistributedRateLimit DistributedRateLimitConfig
}

type YouTubeProducerGlobalBudgetConfig struct {
	Enabled                    bool
	AcquireTimeout             time.Duration
	ActiveInstanceCount        int
	YouTubeScraperMaxInflight  int
	HolodexLiveMaxInflight     int
	BrowserSnapshotMaxInflight int
	BackfillMaxInflight        int
	FallbackMaxInflight        int
	CleanupLimit               int
	WindowCheckEnabled         bool
}

func DefaultYouTubeProducerGlobalBudgetConfig() YouTubeProducerGlobalBudgetConfig {
	return YouTubeProducerGlobalBudgetConfig{
		Enabled:                    false,
		AcquireTimeout:             3 * time.Second,
		ActiveInstanceCount:        0,
		YouTubeScraperMaxInflight:  6,
		HolodexLiveMaxInflight:     4,
		BrowserSnapshotMaxInflight: 1,
		BackfillMaxInflight:        2,
		FallbackMaxInflight:        2,
		CleanupLimit:               128,
		WindowCheckEnabled:         false,
	}
}

type IngestionConfig struct {
	YouTubeEnabled                  bool
	PhotoSyncEnabled                bool
	CommunityShortsBigBangCutoverAt time.Time
}

type ChzzkConfig struct {
	ClientID                  string
	ClientSecret              string
	MaxLivesPageSize          int
	BatchLookupThreshold      int
	MaxConcurrentStatusChecks int
}

type TwitchConfig struct {
	ClientID           string
	ClientSecret       string
	BaseURL            string
	AuthURL            string
	Timeout            time.Duration
	PollInterval       time.Duration
	TokenRefreshSkew   time.Duration
	MarkerTTL          time.Duration
	MaxUsersPerRequest int
}
