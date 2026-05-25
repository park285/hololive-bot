package settings

import "time"

type DistributedRateLimitConfig struct {
	Enabled    bool
	Limit      int
	Window     time.Duration
	KeyPrefix  string
	BucketBase string
}

type HolodexTransportConfig struct {
	MaxConnsPerHost     int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}

type HolodexConcurrencyConfig struct {
	MaxConcurrentRequests int
	OrgAllParallelism     int
	RequestDelay          time.Duration
}

type OfficialScheduleConfig struct {
	BaseURL      string
	Timeout      time.Duration
	CacheExpiry  time.Duration
	PageCacheTTL time.Duration
}

type OfficialProfileConfig struct {
	BaseURL        string
	UserAgent      string
	AcceptLanguage string
	RequestTimeout time.Duration
	DelayBetween   time.Duration
	OutputFile     string
}

func DefaultHolodexOperationalConfig() HolodexConfig {
	return HolodexConfig{
		BaseURL:           "https://holodex.net/api/v2",
		Timeout:           25 * time.Second,
		PerAttemptTimeout: 20 * time.Second,
		MaxRetryAttempts:  3,
		Transport: HolodexTransportConfig{
			MaxConnsPerHost:     50,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     30 * time.Second,
		},
		Concurrency: HolodexConcurrencyConfig{
			MaxConcurrentRequests: 2,
			OrgAllParallelism:     2,
			RequestDelay:          500 * time.Millisecond,
		},
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    true,
			Limit:      10,
			Window:     time.Second,
			KeyPrefix:  "ratelimit:sliding",
			BucketBase: "holodex:api",
		},
	}
}

func DefaultYouTubeOperationalConfig() YouTubeConfig {
	return YouTubeConfig{
		DailyQuotaLimit:         10000,
		SearchQuotaCost:         100,
		ChannelsQuotaCost:       1,
		MaxChannelsPerCall:      20,
		MaxConcurrentRequests:   3,
		SearchMaxResults:        10,
		QuotaSafetyMargin:       2000,
		CacheExpiration:         2 * time.Hour,
		MaxPageBodyBytes:        8 << 20,
		ScraperHTTPTimeout:      15 * time.Second,
		ScraperDialTimeout:      5 * time.Second,
		ScraperHeaderTimeout:    12 * time.Second,
		ScraperPhaseTimeout:     45 * time.Second,
		APIFallbackTimeout:      30 * time.Second,
		CacheSaveTimeout:        5 * time.Second,
		CommunityMissingTTL:     24 * time.Hour,
		VideoRSSBackoffTTL:      6 * time.Hour,
		ProducerRequestInterval: 3 * time.Second,
		ProducerDistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    true,
			Limit:      1,
			Window:     3 * time.Second,
			KeyPrefix:  "ratelimit:sliding",
			BucketBase: "youtube:producer",
		},
	}
}

func DefaultTwitchOperationalConfig() TwitchConfig {
	return TwitchConfig{
		BaseURL:            "https://api.twitch.tv/helix",
		AuthURL:            "https://id.twitch.tv/oauth2/token",
		Timeout:            10 * time.Second,
		PollInterval:       60 * time.Second,
		TokenRefreshSkew:   5 * time.Minute,
		MarkerTTL:          7 * 24 * time.Hour,
		MaxUsersPerRequest: 100,
	}
}

func DefaultChzzkOperationalConfig() ChzzkConfig {
	return ChzzkConfig{
		MaxLivesPageSize:          20,
		BatchLookupThreshold:      4,
		MaxConcurrentStatusChecks: 4,
	}
}

func DefaultOfficialScheduleConfig() OfficialScheduleConfig {
	return OfficialScheduleConfig{
		BaseURL:      "https://schedule.hololive.tv",
		Timeout:      15 * time.Second,
		CacheExpiry:  30 * time.Minute,
		PageCacheTTL: 15 * time.Second,
	}
}

func DefaultOfficialProfileConfig() OfficialProfileConfig {
	return OfficialProfileConfig{
		BaseURL:        "https://hololive.hololivepro.com/talents",
		UserAgent:      "Mozilla/5.0 (compatible; HololiveKakaoBot/1.0; +https://hololive.hololivepro.com)",
		AcceptLanguage: "ja,en;q=0.8,ko;q=0.6",
		RequestTimeout: 15 * time.Second,
		DelayBetween:   350 * time.Millisecond,
		OutputFile:     "../hololive-shared/pkg/domain/internal/model/data/official_profiles_raw.json",
	}
}

const DefaultMaxResponseBodyBytes int64 = 2 << 20 // 2MiB
