package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

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
	PageCacheTTL time.Duration
}

type OfficialScheduleRuntimeConfig struct {
	OfficialSchedule     OfficialScheduleConfig
	MaxResponseBodyBytes int64
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
		MaxPageBodyBytes:     8 << 20,
		ScraperHTTPTimeout:   15 * time.Second,
		ScraperDialTimeout:   5 * time.Second,
		ScraperHeaderTimeout: 12 * time.Second,
		ScraperPhaseTimeout:  45 * time.Second,
		CacheSaveTimeout:     5 * time.Second,
		CommunityMissingTTL:  24 * time.Hour,
		RequestInterval:      3 * time.Second,
		// BucketBase의 youtube:producer는 퇴역 producer 이름이지만 운영 Valkey bucket 식별자라 전환 계획 없이 바꾸지
		// 않는다(유지 사유와 변경 조건은 scraping/state_manager.go의 키 접두사 주석, stack-audit 2026-09-26 C9).
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    true,
			Limit:      1,
			Window:     3 * time.Second,
			KeyPrefix:  "ratelimit:sliding",
			BucketBase: "youtube:producer",
		},
	}
}

func DefaultOfficialScheduleConfig() OfficialScheduleConfig {
	return OfficialScheduleConfig{
		BaseURL:      "https://schedule.hololive.tv",
		Timeout:      15 * time.Second,
		PageCacheTTL: 15 * time.Second,
	}
}

func LoadOfficialScheduleRuntimeConfig() (OfficialScheduleRuntimeConfig, error) {
	officialSchedule, err := loadOfficialScheduleConfig()
	if err != nil {
		return OfficialScheduleRuntimeConfig{}, err
	}

	maxResponseBodyBytes, err := loadMaxResponseBodyBytes()
	if err != nil {
		return OfficialScheduleRuntimeConfig{}, err
	}

	return OfficialScheduleRuntimeConfig{
		OfficialSchedule:     officialSchedule,
		MaxResponseBodyBytes: maxResponseBodyBytes,
	}, nil
}

func loadMaxResponseBodyBytes() (int64, error) {
	maxBytes, err := sharedenv.Int64E("MAX_RESPONSE_BODY_BYTES", DefaultMaxResponseBodyBytes)
	if err != nil {
		return 0, fmt.Errorf("load max response body bytes: %w", err)
	}

	return maxBytes, nil
}

// OfficialScheduleRuntime은 이미 적재한 설정에서 공식 일정 runtime 값을 꺼낸다. 설정이 nil이면 env를 다시 읽던
// 분기는 파싱 오류를 돌려줄 수 없어 지웠다(stack audit B4). 모든 호출자는 적재한 *Config로 부른다.
func (c *Config) OfficialScheduleRuntime() OfficialScheduleRuntimeConfig {
	return OfficialScheduleRuntimeConfig{
		OfficialSchedule:     c.OfficialSchedule,
		MaxResponseBodyBytes: c.MaxResponseBodyBytes,
	}
}

const DefaultMaxResponseBodyBytes int64 = 2 << 20
